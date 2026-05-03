package events

import (
	"log"
	"sync"
	"sync/atomic"
)

type EventOutput interface {
	Write(event DiagnosticEvent) error
	Flush() error
	Close() error
}

type Emitter struct {
	ch      chan DiagnosticEvent
	outputs []EventOutput
	wg      sync.WaitGroup
	dropped atomic.Int64
}

func NewEmitter(bufferSize int, outputs ...EventOutput) *Emitter {
	return &Emitter{
		ch:      make(chan DiagnosticEvent, bufferSize),
		outputs: outputs,
	}
}

func (e *Emitter) Emit(event DiagnosticEvent) {
	select {
	case e.ch <- event:
	default:
		n := e.dropped.Add(1)
		if n%1000 == 0 {
			log.Printf("warn: emitter dropped %d events total", n)
		}
	}
}

func (e *Emitter) Dropped() int64 {
	return e.dropped.Load()
}

func (e *Emitter) Start() {
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		for event := range e.ch {
			for _, out := range e.outputs {
				if err := out.Write(event); err != nil {
					log.Printf("event output write error: %v", err)
				}
			}
		}
		for _, out := range e.outputs {
			if err := out.Flush(); err != nil {
				log.Printf("event output flush error: %v", err)
			}
			if err := out.Close(); err != nil {
				log.Printf("event output close error: %v", err)
			}
		}
	}()
}

func (e *Emitter) Shutdown() {
	close(e.ch)
	e.wg.Wait()
}
