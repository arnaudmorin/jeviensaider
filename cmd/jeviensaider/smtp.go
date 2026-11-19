package main

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"jeviensaider/internal/mail"
)

// smtpFromEnv reads the SMTP settings. It returns nil when
// BENEVOLE_SMTP_HOST is not set: e-mails are then only logged.
//
//	BENEVOLE_SMTP_HOST      smtp.example.org (required to send e-mails)
//	BENEVOLE_SMTP_PORT      587 (default), or 465 for implicit TLS
//	BENEVOLE_SMTP_TLS       starttls (default), tls (default on 465) or none
//	BENEVOLE_SMTP_USERNAME  login, if the server requires authentication
//	BENEVOLE_SMTP_PASSWORD  password (environment only, never a flag)
//	BENEVOLE_SMTP_FROM      sender, default "Je viens aider <noreply@<BASE_URL host>>"
//
// Links in e-mails come from the public URL, so BENEVOLE_BASE_URL is
// required: deriving them from the request's Host header would let anyone
// send organisers links to another site.
func smtpFromEnv(baseURL string) (*mail.Config, error) {
	host := strings.TrimSpace(os.Getenv("BENEVOLE_SMTP_HOST"))
	if host == "" {
		return nil, nil
	}
	if baseURL == "" {
		return nil, fmt.Errorf("BENEVOLE_SMTP_HOST is set: BENEVOLE_BASE_URL (e.g. https://jeviensaider.fr) is required too, for the links in e-mails")
	}
	cfg := &mail.Config{
		Host:     host,
		TLS:      os.Getenv("BENEVOLE_SMTP_TLS"),
		Username: os.Getenv("BENEVOLE_SMTP_USERNAME"),
		Password: os.Getenv("BENEVOLE_SMTP_PASSWORD"),
		From:     strings.TrimSpace(os.Getenv("BENEVOLE_SMTP_FROM")),
	}
	if p := strings.TrimSpace(os.Getenv("BENEVOLE_SMTP_PORT")); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("BENEVOLE_SMTP_PORT=%q: not a number", p)
		}
		cfg.Port = n
	}
	if cfg.From == "" {
		u, err := url.Parse(baseURL)
		if err != nil || u.Hostname() == "" {
			return nil, fmt.Errorf("set BENEVOLE_SMTP_FROM (cannot derive a sender from BENEVOLE_BASE_URL=%q)", baseURL)
		}
		cfg.From = "Je viens aider <noreply@" + u.Hostname() + ">"
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("SMTP settings: %w", err)
	}
	return cfg, nil
}

// checkOpenCreation refuses to send e-mails when anyone can create an
// event: creating one sends a mail to an address the visitor types, which
// would let anybody use the server to send mail to anyone. Set
// BENEVOLE_OPEN_CREATION=true to accept that (on a private network, say);
// the number of such mails is limited anyway.
func checkOpenCreation(smtp *mail.Config, createPassword string) error {
	if smtp == nil || createPassword != "" {
		return nil
	}
	open, err := boolFromEnv("BENEVOLE_OPEN_CREATION", false)
	if err != nil || open {
		return err
	}
	return fmt.Errorf("e-mails are enabled (BENEVOLE_SMTP_HOST) but anyone can create events: set BENEVOLE_CREATE_PASSWORD, " +
		"or BENEVOLE_OPEN_CREATION=true to let anybody make the server e-mail any address")
}
