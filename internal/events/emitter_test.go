package events

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

type captureOutput struct {
	mu     sync.Mutex
	events []DiagnosticEvent
}

func (c *captureOutput) Write(event DiagnosticEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
	return nil
}

func (c *captureOutput) Flush() error { return nil }
func (c *captureOutput) Close() error { return nil }

func (c *captureOutput) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

func TestEmitterNonBlocking(t *testing.T) {
	out := &captureOutput{}
	e := NewEmitter(2, out)
	// Don't start — channel won't drain, so it fills up.

	e.Emit(DiagnosticEvent{EventID: "1"})
	e.Emit(DiagnosticEvent{EventID: "2"})

	done := make(chan struct{})
	go func() {
		e.Emit(DiagnosticEvent{EventID: "3"}) // channel full — must drop, not block
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("Emit blocked when channel was full")
	}

	e.Start()
	e.Shutdown()
}

func TestEmitterDrain(t *testing.T) {
	out := &captureOutput{}
	e := NewEmitter(10, out)
	e.Start()

	for i := range 5 {
		e.Emit(DiagnosticEvent{EventID: fmt.Sprintf("%d", i)})
	}

	e.Shutdown()

	if out.len() != 5 {
		t.Fatalf("expected 5 events written, got %d", out.len())
	}
}

func TestEmitterDropsWhenFull(t *testing.T) {
	out := &captureOutput{}
	e := NewEmitter(2, out)
	// Don't start — nothing drains.

	e.Emit(DiagnosticEvent{EventID: "1"})
	e.Emit(DiagnosticEvent{EventID: "2"})
	e.Emit(DiagnosticEvent{EventID: "3"}) // dropped
	e.Emit(DiagnosticEvent{EventID: "4"}) // dropped

	if got := e.Dropped(); got != 2 {
		t.Fatalf("expected 2 dropped, got %d", got)
	}

	e.Start()
	e.Shutdown()

	if out.len() != 2 {
		t.Fatalf("expected 2 events written (the 2 that fit), got %d", out.len())
	}
}
