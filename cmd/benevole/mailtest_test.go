package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"

	"jeviensaider/internal/mail"
)

type recordSender struct {
	err  error
	sent []mail.Message
}

func (r *recordSender) Send(_ context.Context, m mail.Message) error {
	if r.err != nil {
		return r.err
	}
	r.sent = append(r.sent, m)
	return nil
}

func setSMTPEnv(t *testing.T, host, port string) {
	t.Helper()
	for _, k := range []string{"PORT", "TLS", "USERNAME", "PASSWORD", "FROM"} {
		t.Setenv("BENEVOLE_SMTP_"+k, "")
	}
	t.Setenv("BENEVOLE_SMTP_HOST", host)
	t.Setenv("BENEVOLE_SMTP_PORT", port)
	t.Setenv("BENEVOLE_BASE_URL", "https://jeviensaider.fr")
}

func TestMailTest(t *testing.T) {
	setSMTPEnv(t, "smtp.example.org", "")
	t.Setenv("BENEVOLE_SMTP_USERNAME", "apikey")
	t.Setenv("BENEVOLE_SMTP_PASSWORD", "s3cret")
	rec := &recordSender{}
	newSender := func(cfg mail.Config) (mail.Sender, error) { return rec, nil }

	var out, errb bytes.Buffer
	if code := mailTest([]string{"orga@example.org"}, &out, &errb, newSender); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if len(rec.sent) != 1 || rec.sent[0].To != "orga@example.org" || !strings.Contains(rec.sent[0].Subject, "test") ||
		!strings.Contains(rec.sent[0].Body, "https://jeviensaider.fr") {
		t.Fatalf("sent: %+v", rec.sent)
	}
	for _, want := range []string{"smtp.example.org:587", "starttls", "user apikey", "noreply@jeviensaider.fr", "Sent."} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String()+errb.String(), "s3cret") {
		t.Error("password printed")
	}

	// A failure is reported with the server's error and exit code 1.
	rec.err = errors.New("535 authentication failed")
	out.Reset()
	errb.Reset()
	if code := mailTest([]string{"orga@example.org"}, &out, &errb, newSender); code != 1 || !strings.Contains(errb.String(), "535 authentication failed") {
		t.Errorf("failure: exit %d, stderr %q", code, errb.String())
	}

	// Usage errors.
	for _, args := range [][]string{nil, {"a@b.fr", "c@d.fr"}, {"not an address"}} {
		if code := mailTest(args, io.Discard, io.Discard, newSender); code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
	}
	t.Setenv("BENEVOLE_SMTP_HOST", "")
	errb.Reset()
	if code := mailTest([]string{"a@b.fr"}, io.Discard, &errb, newSender); code != 2 || !strings.Contains(errb.String(), "BENEVOLE_SMTP_HOST") {
		t.Errorf("no SMTP: exit %d, %q", code, errb.String())
	}
}

// End to end, through a real (minimal, unencrypted) SMTP server.
func TestMailTestSMTP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		say := func(s string) { io.WriteString(c, s+"\r\n") }
		say("220 fake")
		var data strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
			case cmd == "DATA":
				say("354 go")
				for {
					l, err := r.ReadString('\n')
					if err != nil || l == ".\r\n" {
						break
					}
					data.WriteString(l)
				}
				say("250 queued")
			case cmd == "QUIT":
				say("221 bye")
				got <- data.String()
				return
			default:
				say("250 ok")
			}
		}
	}()

	host, port, _ := net.SplitHostPort(ln.Addr().String())
	setSMTPEnv(t, host, port)
	t.Setenv("BENEVOLE_SMTP_TLS", "none")
	var out, errb bytes.Buffer
	if code := runMailTest([]string{"orga@example.org"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if d := <-got; !strings.Contains(d, "To: <orga@example.org>") || !strings.Contains(d, "Subject: Je viens aider : e-mail de test") || !strings.Contains(d, "l'envoi des e-mails fonctionne") {
		t.Errorf("message: %q", d)
	}
}
