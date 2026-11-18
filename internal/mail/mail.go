// Package mail sends the app's notification e-mails (to event organisers)
// through an SMTP server, in the background.
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Message is a plain-text e-mail.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Sender delivers a message.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// TLS modes for the SMTP connection.
const (
	TLSStartTLS = "starttls" // plain connection upgraded with STARTTLS (port 587); required
	TLSImplicit = "tls"      // TLS from the start (port 465)
	TLSNone     = "none"     // no encryption: only accepted for a relay on localhost or a private network
)

// Config describes the SMTP server.
type Config struct {
	Host     string
	Port     int    // default 587, or 465 with TLSImplicit
	Username string // empty: no authentication
	Password string
	From     string // "Je viens aider <noreply@example.org>"
	TLS      string // TLSStartTLS (default), TLSImplicit (default on port 465) or TLSNone
	Timeout  time.Duration
}

// Validate fills defaults and checks the configuration.
func (c *Config) Validate() error {
	if c.Host == "" {
		return errors.New("SMTP host is empty")
	}
	c.TLS = strings.ToLower(strings.TrimSpace(c.TLS))
	if c.TLS == "" {
		c.TLS = TLSStartTLS
		if c.Port == 465 {
			c.TLS = TLSImplicit
		}
	}
	switch c.TLS {
	case TLSStartTLS, TLSImplicit, TLSNone:
	default:
		return fmt.Errorf("SMTP TLS mode %q: use starttls, tls or none", c.TLS)
	}
	if c.Port == 0 {
		c.Port = 587
		if c.TLS == TLSImplicit {
			c.Port = 465
		}
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("SMTP port %d is invalid", c.Port)
	}
	from, err := mail.ParseAddress(c.From)
	if err != nil {
		return fmt.Errorf("SMTP from address %q: %v", c.From, err)
	}
	c.From = from.String()
	if c.Username != "" && c.TLS == TLSNone {
		return errors.New("SMTP authentication over an unencrypted connection is refused: use starttls or tls")
	}
	if c.Timeout == 0 {
		c.Timeout = 20 * time.Second
	}
	return nil
}

// SMTP sends messages through an SMTP server.
type SMTP struct {
	cfg Config
	// tlsConfig is used for TLS connections (nil: verify against Host).
	tlsConfig *tls.Config
}

// NewSMTP returns a sender for a validated configuration.
func NewSMTP(cfg Config) (*SMTP, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &SMTP{cfg: cfg}, nil
}

func (s *SMTP) tlsConf() *tls.Config {
	if s.tlsConfig != nil {
		return s.tlsConfig
	}
	return &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}
}

// Send delivers m, honouring ctx and the configured timeout.
func (s *SMTP) Send(ctx context.Context, m Message) error {
	to, err := mail.ParseAddress(m.To)
	if err != nil {
		return permanentError{fmt.Errorf("recipient %q: %v", m.To, err)}
	}
	from, _ := mail.ParseAddress(s.cfg.From)
	data, err := Format(s.cfg.From, to.String(), m.Subject, m.Body, time.Now())
	if err != nil {
		return permanentError{err}
	}

	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	if s.cfg.TLS == TLSNone && !localAddr(conn.RemoteAddr()) {
		conn.Close()
		return permanentError{fmt.Errorf("SMTP TLS mode none is only allowed for a server on this machine or a private network, not %s", conn.RemoteAddr())}
	}
	if s.cfg.TLS == TLSImplicit {
		conn = tls.Client(conn, s.tlsConf())
	}
	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if err := c.Hello(heloName(from.Address)); err != nil {
		return err
	}
	if s.cfg.TLS == TLSStartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("the SMTP server does not offer STARTTLS")
		}
		if err := c.StartTLS(s.tlsConf()); err != nil {
			return err
		}
	}
	if s.cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)); err != nil {
			return err
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return err
	}
	if err := c.Rcpt(to.Address); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// Format builds an RFC 5322 message: UTF-8 text, quoted-printable body,
// encoded subject, CRLF line endings.
func Format(from, to, subject, body string, date time.Time) ([]byte, error) {
	for _, h := range []string{from, to, subject} {
		if strings.ContainsAny(h, "\r\n") {
			return nil, errors.New("line break in a mail header")
		}
	}
	domain := "localhost"
	if a, err := mail.ParseAddress(from); err == nil {
		if _, d, ok := strings.Cut(a.Address, "@"); ok {
			domain = d
		}
	}
	id := make([]byte, 12)
	rand.Read(id)

	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&b, "Date: %s\r\n", date.Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%s@%s>\r\n", hex.EncodeToString(id), domain)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
	b.WriteString("Auto-Submitted: auto-generated\r\n")
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	body = strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")
	if _, err := qp.Write([]byte(body)); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	b.WriteString("\r\n")
	return b.Bytes(), nil
}

// Log is the sender used when no SMTP server is configured: it only logs
// that a message would have been sent. It never logs the body, which
// contains secret links.
type Log struct{ Logger *slog.Logger }

func (l Log) Send(_ context.Context, m Message) error {
	l.Logger.Info("mail not sent (no SMTP server configured)", "to", m.To, "subject", m.Subject)
	return nil
}

// permanentError marks a failure that retrying cannot fix.
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent reports whether err cannot be fixed by retrying: a 5xx reply
// from the SMTP server (unknown recipient, rejected message…) or an invalid
// message.
func Permanent(err error) bool {
	var pe permanentError
	if errors.As(err, &pe) {
		return true
	}
	var te *textproto.Error
	return errors.As(err, &te) && te.Code >= 500
}

// Queue sends messages in the background, so a slow or unreachable SMTP
// server never delays a web page. Temporary failures are retried later
// without holding up the other messages; permanent ones are not retried.
type Queue struct {
	sender  Sender
	log     *slog.Logger
	ch      chan queued
	retries []time.Duration

	mu      sync.Mutex
	closed  bool
	pending sync.WaitGroup // messages not yet sent or given up
	stop    chan struct{}
	ctx     context.Context // cancelled on stop, aborts a send in progress
	cancel  context.CancelFunc
	done    chan struct{}
}

type queued struct {
	m       Message
	attempt int
}

// NewQueue starts a background worker sending through s.
func NewQueue(s Sender, logger *slog.Logger) *Queue {
	ctx, cancel := context.WithCancel(context.Background())
	q := &Queue{
		sender:  s,
		log:     logger,
		ch:      make(chan queued, 200),
		retries: []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute},
		stop:    make(chan struct{}),
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	go q.run()
	return q
}

// Enqueue schedules m and returns immediately. If the queue is full (the
// SMTP server has been down for a while) or closed, the message is dropped
// and logged.
func (q *Queue) Enqueue(m Message) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		q.log.Error("mail queue closed, message dropped", "to", m.To, "subject", m.Subject)
		return
	}
	q.pending.Add(1)
	select {
	case q.ch <- queued{m: m}:
	default:
		q.pending.Done()
		q.log.Error("mail queue full, message dropped", "to", m.To, "subject", m.Subject)
	}
}

func (q *Queue) run() {
	defer close(q.done)
	for {
		select {
		case it := <-q.ch:
			q.deliver(it)
		case <-q.stop:
			return
		}
	}
}

func (q *Queue) deliver(it queued) {
	m := it.m
	err := q.sender.Send(q.ctx, m)
	switch {
	case err == nil:
		q.log.Info("mail sent", "to", m.To, "subject", m.Subject)
		q.pending.Done()
	case Permanent(err):
		q.log.Error("mail rejected, not retried", "to", m.To, "subject", m.Subject, "err", err)
		q.pending.Done()
	case it.attempt >= len(q.retries):
		q.log.Error("mail not sent, giving up", "to", m.To, "subject", m.Subject, "err", err)
		q.pending.Done()
	default:
		d := q.retries[it.attempt]
		q.log.Warn("mail not sent, will retry", "to", m.To, "subject", m.Subject, "err", err, "in", d)
		it.attempt++
		time.AfterFunc(d, func() {
			select {
			case q.ch <- it:
			case <-q.stop:
				q.log.Error("mail not sent, server stopping", "to", m.To, "subject", m.Subject)
				q.pending.Done()
			}
		})
	}
}

// Close stops accepting messages and waits (up to ctx) for the queued ones,
// retries included, to be sent. What is still pending then is abandoned.
func (q *Queue) Close(ctx context.Context) {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.closed = true
	q.mu.Unlock()
	idle := make(chan struct{})
	go func() { q.pending.Wait(); close(idle) }()
	select {
	case <-idle:
	case <-ctx.Done():
	}
	close(q.stop)
	q.cancel()
	<-q.done
}

// localAddr reports whether a is a loopback or private (RFC 1918, ULA)
// address, where an unencrypted connection to a relay is acceptable.
func localAddr(a net.Addr) bool {
	ta, ok := a.(*net.TCPAddr)
	return ok && (ta.IP.IsLoopback() || ta.IP.IsPrivate())
}

// heloName is the name we greet the SMTP server with: the sender's domain.
func heloName(addr string) string {
	if _, d, ok := strings.Cut(addr, "@"); ok && d != "" {
		return d
	}
	return "localhost"
}
