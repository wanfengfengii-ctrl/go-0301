const $ = (id) => document.getElementById(id);

async function refreshStatus() {
  const res = await fetch('/api/status');
  if (!res.ok) return;
  const s = await res.json();
  $('st-service').textContent = s.service;
  $('st-trials').textContent = s.trials.length;
  $('st-now').textContent = new Date(s.now).toISOString();
  renderTrials(s.trials);
}

function renderTrials(trials) {
  const tbody = document.querySelector('#trials-table tbody');
  tbody.innerHTML = '';
  for (const t of trials) {
    const tr = document.createElement('tr');
    tr.innerHTML =
      `<td><code>${t.id}</code></td>` +
      `<td>${t.species}</td>` +
      `<td>${t.current_gen ?? 0}</td>` +
      `<td>${t.locked ? '已锁定' : '未锁定'}</td>` +
      `<td>${terminalLabel(t.terminal)}</td>` +
      `<td>` +
        (t.locked ? '' : `<button data-id="${t.id}" class="lock">锁定</button> `) +
        `<button data-id="${t.id}" class="detail">详情</button>` +
      `</td>`;
    tbody.appendChild(tr);
  }
  for (const btn of tbody.querySelectorAll('button.lock')) {
    btn.addEventListener('click', () => lockTrial(btn.dataset.id));
  }
  for (const btn of tbody.querySelectorAll('button.detail')) {
    btn.addEventListener('click', () => showDetail(btn.dataset.id));
  }
}

function terminalLabel(t) {
  if (!t || t === 'open') return '进行中';
  if (t === 'decided') return '已终局';
  return t;
}

async function lockTrial(id) {
  await fetch(`/api/trials/${encodeURIComponent(id)}/lock`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ version: 'v1' }),
  });
  await refreshStatus();
}

async function showDetail(id) {
  const [trialRes, lineageRes] = await Promise.all([
    fetch(`/api/trials/${encodeURIComponent(id)}`),
    fetch(`/api/trials/${encodeURIComponent(id)}/lineage`),
  ]);
  const trial = await trialRes.json();
  const lineage = await lineageRes.json();
  $('detail-id').textContent = id;
  $('detail-gen').textContent = trial.current_gen ?? 0;
  $('detail-clock').textContent = trial.logical_clock ?? 0;
  $('detail-seedlots').textContent = (lineage.seed_lots || []).length;
  $('detail-samples').textContent = (lineage.samples || []).length;
  $('detail-plates').textContent = (lineage.plates || []).length;
  $('detail-conserved').textContent = lineage.conserved ? '守恒' : '未守恒';
  const al = document.querySelector('#detail-allocations tbody');
  al.innerHTML = '';
  for (const a of lineage.allocations || []) {
    const tr = document.createElement('tr');
    tr.innerHTML =
      `<td>${a.sample_id}</td>` +
      `<td>${a.allocation.source}</td>` +
      `<td>${a.allocation.culture}</td>` +
      `<td>${a.allocation.retain}</td>` +
      `<td>${a.conserved ? '守恒' : '未守恒'}</td>`;
    al.appendChild(tr);
  }
}

$('create-form').addEventListener('submit', async (e) => {
  e.preventDefault();
  const species = $('species').value.trim();
  const key = 'create-' + crypto.randomUUID();
  const res = await fetch('/api/trials', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ species, idempotency_key: key }),
  });
  const body = await res.json();
  $('create-result').textContent =
    res.ok ? `已创建试验 ${body.id}` : `错误：${body.code || ''} ${body.message || ''}`;
  $('species').value = '';
  await refreshStatus();
});

refreshStatus();
