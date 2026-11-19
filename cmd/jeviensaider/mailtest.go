package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	netmail "net/mail"
	"os"
	"strings"
	"time"

	"jeviensaider/internal/mail"
)

const mailTestUsage = `Usage: jeviensaider mail-test [-base-url URL] <address>

Sends a test e-mail to <address> with the server's SMTP settings
(JEVIENSAIDER_SMTP_*), right away and without retries, and prints the SMTP
server's error if it fails. Use it to check the configuration.

Flags:
  -base-url URL         public URL (env JEVIENSAIDER_BASE_URL), required with SMTP
`

// runMailTest implements "jeviensaider mail-test" and returns the exit code.
func runMailTest(args []string, stdout, stderr io.Writer) int {
	return mailTest(args, stdout, stderr, func(cfg mail.Config) (mail.Sender, error) { return mail.NewSMTP(cfg) })
}

func mailTest(args []string, stdout, stderr io.Writer, newSender func(mail.Config) (mail.Sender, error)) int {
	fs := flag.NewFlagSet("jeviensaider mail-test", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, mailTestUsage) }
	baseURL := fs.String("base-url", env("JEVIENSAIDER_BASE_URL", ""), "")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprint(stderr, mailTestUsage)
		return 2
	}
	to, err := netmail.ParseAddress(pos[0])
	if err != nil {
		fmt.Fprintf(stderr, "mail-test: %q is not an e-mail address\n", pos[0])
		return 2
	}

	cfg, err := smtpFromEnv(strings.TrimRight(*baseURL, "/"))
	if err != nil {
		fmt.Fprintln(stderr, "mail-test:", err)
		return 2
	}
	if cfg == nil {
		fmt.Fprintln(stderr, "mail-test: JEVIENSAIDER_SMTP_HOST is not set: e-mails are disabled")
		return 2
	}
	sender, err := newSender(*cfg)
	if err != nil {
		fmt.Fprintln(stderr, "mail-test:", err)
		return 2
	}

	auth := "no authentication"
	if cfg.Username != "" {
		auth = "user " + cfg.Username
	}
	fmt.Fprintf(stdout, "SMTP server: %s:%d (%s, %s)\nFrom:        %s\nTo:          %s\n",
		cfg.Host, cfg.Port, cfg.TLS, auth, cfg.From, to.Address)

	host, _ := os.Hostname()
	now := time.Now()
	msg := mail.Message{
		To:      to.Address,
		Subject: "Je viens aider : e-mail de test",
		Body: "Bonjour,\n\n" +
			"Ceci est un e-mail de test envoyé par « jeviensaider mail-test »" +
			" depuis " + host + " le " + now.Format("02/01/2006 à 15:04:05") + ".\n\n" +
			"Si vous le recevez, l'envoi des e-mails fonctionne.\n" +
			"Adresse du site : " + strings.TrimRight(*baseURL, "/") + "\n\n" +
			"-- \nJe viens aider\n",
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout+5*time.Second)
	defer cancel()
	if err := sender.Send(ctx, msg); err != nil {
		fmt.Fprintln(stderr, "mail-test: not sent:", err)
		return 1
	}
	fmt.Fprintln(stdout, "Sent. Check the inbox (and the spam folder) of", to.Address)
	return 0
}
