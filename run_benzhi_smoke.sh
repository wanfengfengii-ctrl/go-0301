#!/usr/bin/env bash
# Smoke test for the seed-vault viability-release service. It builds the
# server, starts it against a throwaway SQLite database, exercises the public
# JSON API end-to-end (create -> lock -> allocate -> observe -> review ->
# terminal -> credential), and then tears down every process and temporary
# file. It runs entirely against localhost with no external network access and
# never shells out to `go test`.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PORT="${SMOKE_PORT:-18080}"
ADDR="127.0.0.1:${PORT}"
BASE="http://${ADDR}"

TMPDIR="$(mktemp -d)"
BIN="${TMPDIR}/seed-vault"
DB="${TMPDIR}/seed-vault.db"
SERVER_PID=""

cleanup() {
  if [[ -n "${SERVER_PID}" ]] && kill -0 "${SERVER_PID}" 2>/dev/null; then
    kill "${SERVER_PID}" 2>/dev/null || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
  rm -rf "${TMPDIR}"
}
trap cleanup EXIT

echo "building server..."
( cd "${ROOT}" && go build -o "${BIN}" ./cmd/server )

echo "starting server on ${ADDR}..."
ADDR="${ADDR}" DB_PATH="${DB}" "${BIN}" &
SERVER_PID=$!

# Wait for readiness with a bounded retry loop (no external network).
ready=0
for _ in $(seq 1 50); do
  if status="$(curl -sS -o /dev/null -w '%{http_code}' "${BASE}/api/status" 2>/dev/null || true)"; then
    if [[ "${status}" == "200" ]]; then ready=1; break; fi
  fi
  sleep 0.1
done
if [[ "${ready}" != "1" ]]; then
  echo "server did not become ready" >&2
  exit 1
fi

# 1. Create a trial.
create_body="$(curl -sS -X POST "${BASE}/api/trials" \
  -H 'Content-Type: application/json' \
  -d '{"species":"Oryza sativa","idempotency_key":"smoke-1"}')"
trial_id="$(printf '%s' "${create_body}" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')"
if [[ -z "${trial_id}" ]]; then
  echo "create trial failed: ${create_body}" >&2
  exit 1
fi
echo "created trial ${trial_id}"

# 2. Lock the trial.
lock_code="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "${BASE}/api/trials/${trial_id}/lock" \
  -H 'Content-Type: application/json' -d '{"version":"v1"}')"
if [[ "${lock_code}" != "200" ]]; then
  echo "lock failed with status ${lock_code}" >&2
  exit 1
fi
echo "locked trial"

# 3. Allocate samples.
alloc_code="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "${BASE}/api/trials/${trial_id}/samples/allocate" \
  -H 'Content-Type: application/json' \
  -d '{"sample_id":"sample-1","allocation":{"source":100,"culture":60,"retain":20,"measurement":10,"quarantine":5,"loss":5},"seed_lots":[{"id":"lot-1","parent_id":"collection-1","species":"Oryza sativa","location":"cold-1","count":500}],"samples":[{"id":"sample-1","seed_lot_id":"lot-1","count":100,"moisture":8}],"groups":[{"id":"group-1","sample_id":"sample-1","seed_lot_id":"lot-1","generation":1,"count":60}],"plates":[{"id":"plate-1","group_id":"group-1","position":0,"generation":1,"sown":60}]}')"
if [[ "${alloc_code}" != "201" ]]; then
  echo "allocate failed with status ${alloc_code}" >&2
  exit 1
fi
echo "allocated samples"

# 4. Record an observation.
obs_code="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "${BASE}/api/trials/${trial_id}/observations" \
  -H 'Content-Type: application/json' \
  -d '{"plate_id":"plate-1","counts":{"germinated":30,"hard":10},"operator":"op","logical_time":100}')"
if [[ "${obs_code}" != "201" ]]; then
  echo "observe failed with status ${obs_code}" >&2
  exit 1
fi
echo "recorded observation"

# 5. Two independent reviews.
for reviewer in reviewer-1 reviewer-2; do
  rv_code="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "${BASE}/api/trials/${trial_id}/reviews" \
    -H 'Content-Type: application/json' \
    -d "{\"reviewer_id\":\"${reviewer}\",\"qualification\":\"qualified\",\"digest\":\"d-${reviewer}\"}")"
  if [[ "${rv_code}" != "201" ]]; then
    echo "review ${reviewer} failed with status ${rv_code}" >&2
    exit 1
  fi
done
echo "submitted reviews"

# 6. Terminal decision and credential.
term_code="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "${BASE}/api/trials/${trial_id}/terminal" \
  -H 'Content-Type: application/json' -d '{"type":"release"}')"
if [[ "${term_code}" != "201" ]]; then
  echo "terminal failed with status ${term_code}" >&2
  exit 1
fi
cred_body="$(curl -sS "${BASE}/api/trials/${trial_id}/credential")"
if [[ "${cred_body}" != *'"type":"release"'* ]]; then
  echo "credential unexpected: ${cred_body}" >&2
  exit 1
fi
echo "terminal credential: ${cred_body}"

# 7. Frontend entry is served.
frontend_body="$(curl -sS "${BASE}/")"
if [[ "${frontend_body}" != *'种质库'* ]]; then
  echo "frontend entry not served" >&2
  exit 1
fi
echo "frontend served"

echo "smoke test passed"
