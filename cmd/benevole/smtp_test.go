package main

import (
	"strings"
	"testing"

	"benevole-ape/internal/mail"
)

func TestSMTPFromEnv(t *testing.T) {
	clear := func() {
		for _, k := range []string{"HOST", "PORT", "TLS", "USERNAME", "PASSWORD", "FROM"} {
			t.Setenv("BENEVOLE_SMTP_"+k, "")
		}
	}
	clear()
	if cfg, err := smtpFromEnv("https://jeviensaider.fr"); cfg != nil || err != nil {
		t.Errorf("no host: %+v %v", cfg, err)
	}

	t.Setenv("BENEVOLE_SMTP_HOST", "smtp.example.org")
	if _, err := smtpFromEnv(""); err == nil || !strings.Contains(err.Error(), "BENEVOLE_BASE_URL") {
		t.Errorf("missing base URL: %v", err)
	}
	cfg, err := smtpFromEnv("https://jeviensaider.fr")
	if err != nil || cfg.Port != 587 || cfg.TLS != "starttls" || !strings.Contains(cfg.From, "noreply@jeviensaider.fr") {
		t.Errorf("defaults: %+v %v", cfg, err)
	}

	t.Setenv("BENEVOLE_SMTP_PORT", "465")
	t.Setenv("BENEVOLE_SMTP_USERNAME", "apikey")
	t.Setenv("BENEVOLE_SMTP_PASSWORD", "s3cret")
	t.Setenv("BENEVOLE_SMTP_FROM", "APE <ape@ecole.fr>")
	cfg, err = smtpFromEnv("https://jeviensaider.fr")
	if err != nil || cfg.TLS != "tls" || cfg.Username != "apikey" || cfg.Password != "s3cret" || cfg.From != `"APE" <ape@ecole.fr>` {
		t.Errorf("explicit: %+v %v", cfg, err)
	}

	t.Setenv("BENEVOLE_SMTP_PORT", "smtp")
	if _, err := smtpFromEnv("https://jeviensaider.fr"); err == nil {
		t.Error("bad port accepted")
	}
	t.Setenv("BENEVOLE_SMTP_PORT", "25")
	t.Setenv("BENEVOLE_SMTP_TLS", "none")
	if _, err := smtpFromEnv("https://jeviensaider.fr"); err == nil {
		t.Error("password over an unencrypted connection accepted")
	}
}

func TestCheckOpenCreation(t *testing.T) {
	cfg := &mail.Config{Host: "smtp.example.org"}
	t.Setenv("BENEVOLE_OPEN_CREATION", "")
	if checkOpenCreation(nil, "") != nil || checkOpenCreation(cfg, "secret") != nil {
		t.Error("refused a safe configuration")
	}
	if err := checkOpenCreation(cfg, ""); err == nil || !strings.Contains(err.Error(), "BENEVOLE_CREATE_PASSWORD") {
		t.Errorf("open creation with e-mails: %v", err)
	}
	t.Setenv("BENEVOLE_OPEN_CREATION", "true")
	if checkOpenCreation(cfg, "") != nil {
		t.Error("explicit opt-in refused")
	}
}
