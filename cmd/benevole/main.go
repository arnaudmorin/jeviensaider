// Command benevole serves the APE volunteer sign-up web app.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
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
	if len(os.Args) > 1 && os.Args[1] == "events" {
		os.Exit(runEvents(os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
	}
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage:\n  benevole [flags]           run the web server\n  benevole events <command>  super-admin tool: list, show, delete events (see \"benevole events help\")\n\nFlags:\n")
		flag.PrintDefaults()
	}
	trustDefault, _ := strconv.ParseBool(env("BENEVOLE_TRUST_PROXY", "false"))
	retentionDefault, err := strconv.Atoi(env("BENEVOLE_RETENTION_DAYS", "60"))
	if err != nil {
		retentionDefault = 60
	}
	addr := flag.String("addr", env("BENEVOLE_ADDR", ":8080"), "listen address (env BENEVOLE_ADDR)")
	dbPath := flag.String("db", env("BENEVOLE_DB", "benevole.db"), "SQLite database file (env BENEVOLE_DB)")
	baseURL := flag.String("base-url", env("BENEVOLE_BASE_URL", ""), "public URL, e.g. https://benevoles.example.org (env BENEVOLE_BASE_URL)")
	trustProxy := flag.Bool("trust-proxy", trustDefault, "trust X-Forwarded-* headers from a reverse proxy (env BENEVOLE_TRUST_PROXY)")
	retention := flag.Int("retention-days", retentionDefault, "delete events this many days after their last day of activity, 0 to keep them forever (env BENEVOLE_RETENTION_DAYS)")
	healthcheck := flag.Bool("healthcheck", false, "query /healthz on the listen address and exit 0 if healthy (for container health checks)")
	flag.Parse()
	if *healthcheck {
		os.Exit(checkHealth(*addr))
	}
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
		RetentionDays:  *retention,
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

	if *retention > 0 {
		go cleanupLoop(ctx, st, *retention, logger)
	}

	go func() {
		logger.Info("listening", "addr", *addr, "db", *dbPath, "create_password", createPassword != "", "retention_days", *retention)
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

// checkHealth calls /healthz on the local listen address. It lets minimal
// container images (no curl or wget) run a health check with the binary itself.
func checkHealth(addr string) int {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: status", resp.StatusCode)
		return 1
	}
	return 0
}

// cleanupLoop deletes expired events at startup, then every hour.
func cleanupLoop(ctx context.Context, st *store.Store, days int, logger *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		n, err := st.Cleanup(ctx, days, time.Now())
		switch {
		case err != nil && ctx.Err() == nil:
			logger.Error("cleanup", "err", err)
		case n > 0:
			logger.Info("cleanup: deleted expired events", "count", n, "retention_days", days)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
