// Command benevole serves the APE volunteer sign-up web app.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"benevole-ape/internal/store"
	"benevole-ape/internal/web"
)

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func main() {
	trustDefault, _ := strconv.ParseBool(env("BENEVOLE_TRUST_PROXY", "false"))
	addr := flag.String("addr", env("BENEVOLE_ADDR", ":8080"), "listen address (env BENEVOLE_ADDR)")
	dbPath := flag.String("db", env("BENEVOLE_DB", "benevole.db"), "SQLite database file (env BENEVOLE_DB)")
	baseURL := flag.String("base-url", env("BENEVOLE_BASE_URL", ""), "public URL, e.g. https://benevoles.example.org (env BENEVOLE_BASE_URL)")
	trustProxy := flag.Bool("trust-proxy", trustDefault, "trust X-Forwarded-* headers from a reverse proxy (env BENEVOLE_TRUST_PROXY)")
	flag.Parse()
	// The creation password is only read from the environment, so it never
	// shows up in `ps` output.
	createPassword := os.Getenv("BENEVOLE_CREATE_PASSWORD")

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	st, err := store.Open(*dbPath)
	if err != nil {
		logger.Error("open database", "path", *dbPath, "err", err)
		os.Exit(1)
	}
	defer st.Close()

	srv, err := web.New(st, web.Config{
		BaseURL:        *baseURL,
		CreatePassword: createPassword,
		TrustProxy:     *trustProxy,
		Logger:         logger,
	})
	if err != nil {
		logger.Error("init web", "err", err)
		os.Exit(1)
	}

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("listening", "addr", *addr, "db", *dbPath, "create_password", createPassword != "")
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", "err", err)
	}
}
