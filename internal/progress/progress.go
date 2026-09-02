// Package progress defines UI-independent lifecycle progress events.
package progress

import (
	"bytes"
	"context"
	"io"
	"sync"
)

// Kind identifies how a progress event should be presented.
type Kind uint8

const (
	Stage Kind = iota
	Activity
	Readiness
	Warning
)

// Event is a structured lifecycle update. Message must not contain manifest or
// secret contents; renderers apply a second redaction boundary before display.
type Event struct {
	Kind    Kind
	Message string
}

// Reporter consumes lifecycle progress updates. Implementations must be safe
// for concurrent use because subprocess stdout and stderr can arrive together.
type Reporter interface {
	Report(Event)
}

type contextKey struct{}

// WithReporter attaches a reporter to an operation context.
func WithReporter(ctx context.Context, reporter Reporter) context.Context {
	if reporter == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, reporter)
}

// ReporterFromContext returns the reporter attached to an operation.
func ReporterFromContext(ctx context.Context) (Reporter, bool) {
	reporter, ok := ctx.Value(contextKey{}).(Reporter)
	return reporter, ok && reporter != nil
}

// Report sends an event when the operation has a progress reporter.
func Report(ctx context.Context, kind Kind, message string) {
	if reporter, ok := ReporterFromContext(ctx); ok {
		reporter.Report(Event{Kind: kind, Message: message})
	}
}

// LineWriter converts newline-delimited subprocess output into activity events.
// It deliberately retains only a bounded partial line.
type LineWriter struct {
	ctx     context.Context
	mu      sync.Mutex
	partial []byte
}

const maxPartialLine = 4096

// NewLineWriter returns a subprocess output observer.
func NewLineWriter(ctx context.Context) *LineWriter { return &LineWriter{ctx: ctx} }

// Write implements io.Writer. Reporter failures cannot affect the subprocess.
func (w *LineWriter) Write(value []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	originalLength := len(value)
	for len(value) > 0 {
		index := bytes.IndexByte(value, '\n')
		if index < 0 {
			w.appendPartial(value)
			break
		}
		w.appendPartial(value[:index])
		w.emitLocked()
		value = value[index+1:]
	}
	return originalLength, nil
}

func (w *LineWriter) appendPartial(value []byte) {
	remaining := maxPartialLine - len(w.partial)
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
		}
		w.partial = append(w.partial, value...)
	}
}

func (w *LineWriter) emitLocked() {
	if len(w.partial) > 0 {
		Report(w.ctx, Activity, string(w.partial))
	}
	w.partial = w.partial[:0]
}

// Close emits a final unterminated line.
func (w *LineWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.emitLocked()
	return nil
}

var _ io.WriteCloser = (*LineWriter)(nil)
