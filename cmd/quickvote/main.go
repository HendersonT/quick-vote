// Command quickvote runs the Quick Vote server: a single binary embedding the
// React SPA and persisting all state to a SQLite file.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/quickvote/quickvote/internal/server"
	"github.com/quickvote/quickvote/internal/store"
	"github.com/quickvote/quickvote/webembed"
)

func main() {
	defaultAddr := envOr("QV_ADDR", ":8080")
	defaultDB := envOr("QV_DB", "/data/quickvote.db")

	addr := flag.String("addr", defaultAddr, "listen address (env QV_ADDR)")
	dbPath := flag.String("db", defaultDB, "path to the SQLite database file (env QV_DB)")
	flag.Parse()

	if dir := filepath.Dir(*dbPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("quickvote: create db directory %q: %v", dir, err)
		}
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("quickvote: open store: %v", err)
	}
	defer st.Close()

	srv := server.New(st, webembed.FS())

	log.Printf("quickvote: listening on %s, db at %s", *addr, *dbPath)
	if err := http.ListenAndServe(*addr, srv); err != nil {
		log.Fatalf("quickvote: server error: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
