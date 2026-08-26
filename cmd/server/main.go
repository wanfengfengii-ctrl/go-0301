// Command server is the executable entry point for the seed-vault viability
// verification service. It opens the durable SQLite store, recovers any
// previously persisted events, wires the rules catalogue into the application
// service, and exposes the stable JSON API plus the built frontend.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"seed-vault-viability-release/internal/httpapi"
	"seed-vault-viability-release/internal/rules"
	"seed-vault-viability-release/internal/service"
	"seed-vault-viability-release/internal/store"
)

func main() {
	addr := flag.String("addr", envOr("ADDR", ":8080"), "listen address")
	dbPath := flag.String("db", envOr("DB_PATH", "seed-vault.db"), "sqlite database path")
	flag.Parse()

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	svc := service.New(st, rules.NewStandardCatalog())
	if err := svc.Recover(); err != nil {
		log.Fatalf("recover: %v", err)
	}
	log.Printf("seed-vault-viability-release recovered %d trials", len(svc.TrialSummaries()))

	srv := httpapi.NewServer(svc)

	log.Printf("seed-vault-viability-release listening on %s (db %s)", *addr, *dbPath)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
