// Command benevole serves the APE volunteer sign-up web app.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"benevole-ape/internal/mail"
	"benevole-ape/internal/store"
	"benevole-ape/internal/web"
)

// env returns an environment variable, or def when it is unset or empty
// (compose files often pass empty values for unset ones).
func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "events" {
		os.Exit(runEvents(os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
	}
	if len(os.Args) > 1 && os.Args[1] == "mail-test" {
		os.Exit(runMailTest(os.Args[2:], os.Stdout, os.Stderr))
	}
	cfg, err := loadConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration:", err)
		os.Exit(2)
	}
	if cfg.healthcheck {
		os.Exit(checkHealth(cfg.addr))
	}
	os.Exit(serve(cfg))
}

// serve runs the web server until SIGINT or SIGTERM, and returns the exit
// code.
func serve(cfg *config) int {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	var sender mail.Sender = mail.Log{Logger: logger}
	if cfg.smtp != nil {
		sender, _ = mail.NewSMTP(*cfg.smtp) // already validated
		l := cfg.mailLimits
		logger.Info("e-mails enabled", "smtp", cfg.smtp.Host, "port", cfg.smtp.Port, "tls", cfg.smtp.TLS, "from", cfg.smtp.From,
			"limits", fmt.Sprintf("%d created/address/day, %d created/hour, %d sign-ups/event/hour, %d total/hour (0 = unlimited)",
				l.CreatedPerAddressPerDay, l.CreatedPerHour, l.SignupsPerEventPerHour, l.PerHour))
	} else {
		logger.Warn("e-mails disabled: set BENEVOLE_SMTP_HOST to send them (they are only logged)")
	}
	mailQueue := mail.NewQueue(sender, logger)

	st, err := store.Open(cfg.dbPath)
	if err != nil {
		logger.Error("open database", "path", cfg.dbPath, "err", err)
		return 1
	}
	defer st.Close()

	srv, err := web.New(st, web.Config{
		BaseURL:        cfg.baseURL,
		CreatePassword: cfg.createPassword,
		TrustProxy:     cfg.trustProxy,
		Retention:      cfg.retention,
		Mailer:         mailQueue,
		MailEnabled:    cfg.smtp != nil,
		MailLimits:     &cfg.mailLimits,
		Logger:         logger,
	})
	if err != nil {
		logger.Error("init web", "err", err)
		return 1
	}
	httpSrv := &http.Server{
		Addr:              cfg.addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cfg.retention.Enabled() {
		go cleanupLoop(ctx, st, cfg.retention, logger)
	} else {
		logger.Info("automatic deletion of events is disabled: events are kept forever")
	}
	go func() {
		logger.Info("listening", "addr", cfg.addr, "db", cfg.dbPath, "create_password", cfg.createPassword != "", "retention_days", cfg.retention.DefaultDays, "retention_max_days", cfg.retention.Max())
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
	mailQueue.Close(shutdownCtx) // send what is queued, within the same 10 s
	return 0
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
func cleanupLoop(ctx context.Context, st *store.Store, r store.Retention, logger *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		n, err := st.Cleanup(ctx, r, time.Now())
		switch {
		case err != nil && ctx.Err() == nil:
			logger.Error("cleanup", "err", err)
		case n > 0:
			logger.Info("cleanup: deleted expired events", "count", n, "retention_days", r.DefaultDays, "retention_max_days", r.Max())
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
