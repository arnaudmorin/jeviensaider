package web

import (
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"benevole-ape/internal/mail"
)

// gatedMailer holds each message in Enqueue while blocked is set. It shows
// what the answer of the page does not wait for.
type gatedMailer struct {
	fakeMailer
	gate    chan struct{}
	blocked atomic.Bool
}

func (g *gatedMailer) Enqueue(m mail.Message) {
	if g.blocked.Load() {
		<-g.gate
	}
	g.fakeMailer.Enqueue(m)
}

func gatedRecoverEnv(t *testing.T, limits *MailLimits) (*env, *gatedMailer) {
	t.Helper()
	gm := &gatedMailer{gate: make(chan struct{})}
	e := newEnv(t, Config{Mailer: gm, MailEnabled: true, BaseURL: "https://jeviensaider.example", MailLimits: limits})
	createFor(t, e, "Fête", "orga@example.org")
	gm.take()
	gm.blocked.Store(true)
	return e, gm
}

// The look-up and the mail are slower for an address that organises events.
// The answer must not wait for them, or its delay would tell a visitor which
// addresses are organisers.
func TestRecoverAnswersBeforeTheMailIsQueued(t *testing.T) {
	e, gm := gatedRecoverEnv(t, nil)

	type result struct {
		code int
		err  error
	}
	done := make(chan result, 1)
	go func() {
		res, err := e.client().PostForm(e.ts.URL+"/recover", url.Values{"email": {"orga@example.org"}})
		if err != nil {
			done <- result{err: err}
			return
		}
		res.Body.Close()
		done <- result{code: res.StatusCode}
	}()
	select {
	case r := <-done:
		if r.err != nil || r.code != http.StatusSeeOther {
			t.Fatalf("answer: status %d, error %v", r.code, r.err)
		}
	case <-time.After(3 * time.Second):
		close(gm.gate) // free the handler that is stuck
		t.Fatal("the answer waited for the mail to be queued")
	}

	close(gm.gate)
	e.srv.Wait()
	if msgs := gm.take(); len(msgs) != 1 || msgs[0].To != "orga@example.org" {
		t.Errorf("messages: %+v", msgs)
	}
}

// Requests that arrive while too many are being served in the background are
// dropped, so that a flood cannot pile up work.
func TestRecoverBackgroundWorkIsBounded(t *testing.T) {
	e, gm := gatedRecoverEnv(t, &MailLimits{})
	c := e.client()
	for i := 0; i < maxBackground+3; i++ {
		res, err := c.PostForm(e.ts.URL+"/recover", url.Values{"email": {"orga@example.org"}})
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusSeeOther {
			t.Fatalf("request %d: status %d", i, res.StatusCode)
		}
	}
	close(gm.gate)
	e.srv.Wait()
	if n := len(gm.take()); n != maxBackground {
		t.Errorf("%d mails, want %d: the work that is not bounded or the dropped requests that are not dropped", n, maxBackground)
	}
}
