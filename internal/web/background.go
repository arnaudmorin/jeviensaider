package web

import (
	"context"
	"time"
)

// maxBackground is how many jobs that handlers started can run at the same
// time. A request that arrives when all the places are taken is dropped.
const maxBackground = 8

// backgroundTimeout is how long one job can run.
const backgroundTimeout = 10 * time.Second

// background runs job in its own goroutine, so that a handler can answer
// before the work is done. It returns false, and does not run the job, when
// maxBackground jobs already run. The context of the job keeps the values of
// parent, but the end of the request does not cancel it.
func (s *Server) background(parent context.Context, job func(ctx context.Context)) bool {
	select {
	case s.bgSlots <- struct{}{}:
	default:
		return false
	}
	s.bg.Add(1)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), backgroundTimeout)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				s.log.Error("background job panicked", "panic", p)
			}
			cancel()
			<-s.bgSlots
			s.bg.Done()
		}()
		job(ctx)
	}()
	return true
}

// Wait blocks until the jobs that handlers started in the background end.
// Call it after the HTTP server stops, and before the store and the mail
// queue close.
func (s *Server) Wait() { s.bg.Wait() }
