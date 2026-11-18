package mail

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFormat(t *testing.T) {
	b, err := Format("Je viens aider <noreply@example.org>", "orga@example.org",
		"Inscription : Zoé – Pêche aux canards", "Bonjour,\nZoé s'est inscrite. Lien : https://example.org/a/xyz\n",
		time.Date(2026, 6, 20, 14, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(strings.ReplaceAll(s, "\r\n", ""), "\n") {
		t.Error("bare LF in the message")
	}
	msg, err := mail.ReadMessage(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	subj, err := new(mimeDecoder).decode(msg.Header.Get("Subject"))
	if err != nil || subj != "Inscription : Zoé – Pêche aux canards" {
		t.Errorf("subject %q, %v", subj, err)
	}
	if msg.Header.Get("Content-Type") != "text/plain; charset=utf-8" || msg.Header.Get("Message-ID") == "" {
		t.Errorf("headers: %v", msg.Header)
	}
	body, _ := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if !strings.Contains(string(body), "Zoé s'est inscrite.") || !strings.Contains(string(body), "https://example.org/a/xyz") {
		t.Errorf("body: %q", body)
	}

	if _, err := Format("a@b.fr", "x@y.fr\r\nBcc: evil@z.fr", "s", "b", time.Now()); err == nil {
		t.Error("header injection accepted")
	}
}

func TestConfigValidate(t *testing.T) {
	c := Config{Host: "smtp.example.org", From: "noreply@example.org"}
	if err := c.Validate(); err != nil || c.Port != 587 || c.TLS != TLSStartTLS {
		t.Errorf("defaults: %+v %v", c, err)
	}
	c = Config{Host: "smtp.example.org", Port: 465, From: "noreply@example.org"}
	if err := c.Validate(); err != nil || c.TLS != TLSImplicit {
		t.Errorf("465: %+v %v", c, err)
	}
	for _, bad := range []Config{
		{From: "noreply@example.org"},
		{Host: "h", From: "not an address"},
		{Host: "h", From: "a@b.fr", TLS: "ssl3"},
		{Host: "h", From: "a@b.fr", TLS: TLSNone, Username: "u", Password: "p"},
		{Host: "h", From: "a@b.fr", Port: 70000},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("accepted: %+v", bad)
		}
	}
}

// fakeSMTP is a minimal SMTP server recording the last message.
type fakeSMTP struct {
	ln        net.Listener
	starttls  bool
	tlsConf   *tls.Config // for STARTTLS
	mu        sync.Mutex
	from, rcp string
	data      string
}

func newFakeSMTP(t *testing.T, starttls bool) *fakeSMTP {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{ln: ln, starttls: starttls}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return f
}

func (f *fakeSMTP) serve(c net.Conn) {
	defer func() { c.Close() }()
	r := bufio.NewReader(c)
	say := func(s string) { io.WriteString(c, s+"\r\n") }
	say("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"):
			if f.starttls {
				say("250-fake")
				say("250 STARTTLS")
			} else {
				say("250 fake")
			}
		case cmd == "STARTTLS" && f.starttls:
			say("220 go ahead")
			c = tls.Server(c, f.tlsConf)
			r = bufio.NewReader(c)
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			f.mu.Lock()
			f.from = strings.TrimSpace(line[10:])
			f.mu.Unlock()
			say("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			f.mu.Lock()
			f.rcp = strings.TrimSpace(line[8:])
			f.mu.Unlock()
			say("250 ok")
		case cmd == "DATA":
			say("354 go")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			f.mu.Lock()
			f.data = b.String()
			f.mu.Unlock()
			say("250 queued")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("502 unknown")
		}
	}
}

func (f *fakeSMTP) config(tlsMode string) Config {
	host, port, _ := net.SplitHostPort(f.ln.Addr().String())
	p, _ := strconv.Atoi(port)
	return Config{Host: host, Port: p, TLS: tlsMode, From: "Je viens aider <noreply@example.org>", Timeout: 5 * time.Second}
}

func TestSMTPSend(t *testing.T) {
	f := newFakeSMTP(t, false)
	s, err := NewSMTP(f.config(TLSNone))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Send(context.Background(), Message{To: "orga@example.org", Subject: "Événement créé", Body: "Bonjour"}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.from != "<noreply@example.org>" || f.rcp != "<orga@example.org>" || !strings.Contains(f.data, "Bonjour") {
		t.Errorf("from %q rcpt %q data %q", f.from, f.rcp, f.data)
	}
}

func TestSMTPRequiresStartTLS(t *testing.T) {
	f := newFakeSMTP(t, false) // does not offer STARTTLS
	s, err := NewSMTP(f.config(TLSStartTLS))
	if err != nil {
		t.Fatal(err)
	}
	err = s.Send(context.Background(), Message{To: "orga@example.org", Subject: "s", Body: "b"})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("want a STARTTLS error, got %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.data != "" {
		t.Error("message sent in clear text")
	}
}

type flakySender struct {
	mu    sync.Mutex
	fails int
	sent  []Message
}

func (f *flakySender) Send(_ context.Context, m Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fails > 0 {
		f.fails--
		return errors.New("temporary failure")
	}
	f.sent = append(f.sent, m)
	return nil
}

func TestQueueRetriesAndCloses(t *testing.T) {
	f := &flakySender{fails: 2}
	q := NewQueue(f, slog.New(slog.NewTextHandler(io.Discard, nil)))
	q.retries = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	q.Enqueue(Message{To: "a@b.fr", Subject: "1"})
	q.Enqueue(Message{To: "a@b.fr", Subject: "2"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	q.Close(ctx)
	if len(f.sent) != 2 {
		t.Fatalf("sent %d messages, want 2", len(f.sent))
	}
}

// mimeDecoder decodes RFC 2047 words.
type mimeDecoder struct{}

func (mimeDecoder) decode(s string) (string, error) { return new(mime.WordDecoder).DecodeHeader(s) }

// selfSigned returns a server certificate for 127.0.0.1 and a client
// configuration trusting it.
func selfSigned(t *testing.T) (server, client *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}},
		&tls.Config{RootCAs: pool, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12}
}

func TestSMTPSendEncrypted(t *testing.T) {
	serverTLS, clientTLS := selfSigned(t)

	// STARTTLS (port 587 style).
	f := newFakeSMTP(t, true)
	f.tlsConf = serverTLS
	s, err := NewSMTP(f.config(TLSStartTLS))
	if err != nil {
		t.Fatal(err)
	}
	s.tlsConfig = clientTLS
	if err := s.Send(context.Background(), Message{To: "orga@example.org", Subject: "s", Body: "via starttls"}); err != nil {
		t.Fatalf("starttls: %v", err)
	}
	f.mu.Lock()
	if !strings.Contains(f.data, "via starttls") {
		t.Errorf("starttls data %q", f.data)
	}
	f.mu.Unlock()

	// Implicit TLS (port 465 style).
	ln, err := tls.Listen("tcp", "127.0.0.1:0", serverTLS)
	if err != nil {
		t.Fatal(err)
	}
	g := &fakeSMTP{ln: ln}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go g.serve(c)
		}
	}()
	s, err = NewSMTP(g.config(TLSImplicit))
	if err != nil {
		t.Fatal(err)
	}
	s.tlsConfig = clientTLS
	if err := s.Send(context.Background(), Message{To: "orga@example.org", Subject: "s", Body: "via tls"}); err != nil {
		t.Fatalf("implicit tls: %v", err)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !strings.Contains(g.data, "via tls") {
		t.Errorf("tls data %q", g.data)
	}

	// A certificate that does not match is refused.
	s.tlsConfig = nil // verify against the system roots: the self-signed cert fails
	if err := s.Send(context.Background(), Message{To: "orga@example.org", Subject: "s", Body: "x"}); err == nil {
		t.Error("untrusted certificate accepted")
	}
}

// scriptedSender fails according to a function of the recipient.
type scriptedSender struct {
	mu    sync.Mutex
	fail  func(Message, int) error // message, attempt number for it
	tries map[string]int
	sent  []string
}

func (s *scriptedSender) Send(_ context.Context, m Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tries == nil {
		s.tries = map[string]int{}
	}
	n := s.tries[m.To]
	s.tries[m.To]++
	if err := s.fail(m, n); err != nil {
		return err
	}
	s.sent = append(s.sent, m.To)
	return nil
}

func TestQueuePermanentErrorsAndNoHeadOfLineBlocking(t *testing.T) {
	s := &scriptedSender{fail: func(m Message, n int) error {
		switch m.To {
		case "unknown@b.fr":
			return &textproto.Error{Code: 550, Msg: "no such user"}
		case "slow@b.fr":
			if n == 0 {
				return &textproto.Error{Code: 451, Msg: "try again later"}
			}
		}
		return nil
	}}
	q := NewQueue(s, slog.New(slog.NewTextHandler(io.Discard, nil)))
	q.retries = []time.Duration{200 * time.Millisecond}
	q.Enqueue(Message{To: "unknown@b.fr"})
	q.Enqueue(Message{To: "slow@b.fr"})
	q.Enqueue(Message{To: "ok@b.fr"})

	// ok@ is sent while slow@ waits for its retry.
	deadline := time.Now().Add(150 * time.Millisecond)
	for {
		s.mu.Lock()
		sent := append([]string(nil), s.sent...)
		s.mu.Unlock()
		if len(sent) == 1 && sent[0] == "ok@b.fr" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ok@ blocked behind a retry: sent %v", sent)
		}
		time.Sleep(5 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	q.Close(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tries["unknown@b.fr"] != 1 {
		t.Errorf("permanent error retried: %d tries", s.tries["unknown@b.fr"])
	}
	if len(s.sent) != 2 || s.sent[1] != "slow@b.fr" {
		t.Errorf("sent %v, want ok@ then slow@ (after its retry)", s.sent)
	}
	q.Enqueue(Message{To: "late@b.fr"}) // after Close: dropped, no panic
}

func TestPermanentAndLocalAddr(t *testing.T) {
	if !Permanent(&textproto.Error{Code: 554}) || Permanent(&textproto.Error{Code: 421}) || Permanent(errors.New("dial: timeout")) {
		t.Error("Permanent misclassifies")
	}
	for addr, want := range map[string]bool{"127.0.0.1": true, "::1": true, "10.1.2.3": true, "192.168.1.43": true, "172.18.0.2": true, "fd00::1": true, "203.0.113.5": false, "2001:db8::1": false} {
		if got := localAddr(&net.TCPAddr{IP: net.ParseIP(addr)}); got != want {
			t.Errorf("localAddr(%s) = %v", addr, got)
		}
	}
}
