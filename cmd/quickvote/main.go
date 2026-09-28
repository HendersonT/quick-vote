// Command quickvote runs the Quick Vote server: a single binary embedding the
// React SPA and persisting all state to a SQLite file.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/HendersonT/quick-vote/internal/server"
	"github.com/HendersonT/quick-vote/internal/store"
	"github.com/HendersonT/quick-vote/webembed"
)

func main() {
	defaultAddr := envOr("QV_ADDR", ":8080")
	defaultDB := envOr("QV_DB", "/data/quickvote.db")

	addr := flag.String("addr", defaultAddr, "listen address (env QV_ADDR)")
	dbPath := flag.String("db", defaultDB, "path to the SQLite database file (env QV_DB)")
	retentionDays := flag.Int("retention-days", envInt("QV_RETENTION_DAYS", 90),
		"delete votes older than this many days; 0 keeps them forever (env QV_RETENTION_DAYS)")
	trustedIPHeader := flag.String("trusted-ip-header", os.Getenv("QV_TRUSTED_IP_HEADER"),
		"header carrying the real client IP from a trusted reverse proxy, e.g. CF-Connecting-IP; "+
			"only set when the server is reachable solely through that proxy (env QV_TRUSTED_IP_HEADER)")
	allowedOrigins := flag.String("allowed-origins", os.Getenv("QV_ALLOWED_ORIGINS"),
		"comma-separated extra origins allowed to open WebSockets, e.g. http://localhost:5173 (env QV_ALLOWED_ORIGINS)")
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

	srv := server.NewWithConfig(st, webembed.FS(), server.Config{
		TrustedIPHeader: strings.TrimSpace(*trustedIPHeader),
		AllowedOrigins:  splitList(*allowedOrigins),
	})

	// Re-arm any phase timers that were in flight before this process started,
	// so a restart doesn't silently strand votes waiting on a deadline.
	if err := srv.RearmTimers(); err != nil {
		log.Fatalf("quickvote: re-arm timers: %v", err)
	}

	srv.StartPruning(time.Duration(*retentionDays) * 24 * time.Hour)

	httpSrv := &http.Server{
		Addr:    *addr,
		Handler: srv,
		// Timeouts bound how long a slow or idle client can hold a
		// connection (slowloris). WebSocket upgrades are unaffected: the
		// upgrader clears these deadlines once the connection is hijacked.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	log.Printf("quickvote: listening on %s, db at %s", *addr, *dbPath)
	if err := httpSrv.ListenAndServe(); err != nil {
		log.Fatalf("quickvote: server error: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Fatalf("quickvote: %s must be an integer, got %q", key, v)
	}
	return n
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
