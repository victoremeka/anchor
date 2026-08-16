// Runs the Anchor Engine as a standalone HTTP server
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"anchor/internal/engine"
	"anchor/internal/httpapi"
	"anchor/internal/registry"
	"anchor/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	dsn := os.Getenv("ANCHOR_DATABASE_URL")
	if dsn == "" {
		log.Error("ANCHOR_DATABASE_URL is required")
		os.Exit(1)
	}
	addr := envOr("ANCHOR_LISTEN_ADDR", "127.0.0.1:8080")

	leaseTTL, err := envDurationSeconds("ANCHOR_LEASE_TTL_SECONDS", 60*time.Second)
	if err != nil {
		log.Error("invalid ANCHOR_LEASE_TTL_SECONDS", "err", err)
		os.Exit(1)
	}
	sweepInterval, err := envDurationSeconds("ANCHOR_RECLAIM_SWEEP_INTERVAL_SECONDS", 15*time.Second)
	if err != nil {
		log.Error("invalid ANCHOR_RECLAIM_SWEEP_INTERVAL_SECONDS", "err", err)
		os.Exit(1)
	}
	maxDBConns, err := envInt("ANCHOR_DB_MAX_CONNS", 10)
	if err != nil {
		log.Error("invalid ANCHOR_DB_MAX_CONNS", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	s, err := store.Open(ctx, dsn, int32(maxDBConns))
	if err != nil {
		log.Error("store.Open", "err", err)
		os.Exit(1)
	}
	defer s.Close()

	e := engine.New(s, registry.New(), leaseTTL)
	e.StartReclaimLoop(ctx, sweepInterval, func(err error) {
		log.Error("reclaim sweep failed", "err", err)
	})

	server := &http.Server{
		Addr:              addr,
		Handler:           httpapi.New(e, log).Mux(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Error("server shutdown", "err", err)
		}
	}()

	log.Info("anchor engine listening", "addr", addr, "lease_ttl", leaseTTL, "reclaim_sweep_interval", sweepInterval)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("listen and serve", "err", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envDurationSeconds(key string, fallback time.Duration) (time.Duration, error) {
	secs, err := envInt(key, int(fallback/time.Second))
	if err != nil {
		return 0, err
	}
	return time.Duration(secs) * time.Second, nil
}

func envInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	return strconv.Atoi(v)
}
