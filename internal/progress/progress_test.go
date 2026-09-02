package progress

import (
	"context"
	"strings"
	"testing"
)

type recorder struct{ events []Event }

func (r *recorder) Report(event Event) { r.events = append(r.events, event) }

func TestLineWriterEmitsLinesAndBoundsPartialInput(t *testing.T) {
	t.Parallel()
	reporter := &recorder{}
	writer := NewLineWriter(WithReporter(context.Background(), reporter))
	_, _ = writer.Write([]byte("first\n" + strings.Repeat("x", maxPartialLine+100)))
	_ = writer.Close()
	if len(reporter.events) != 2 || reporter.events[0].Message != "first" || len(reporter.events[1].Message) != maxPartialLine {
		t.Fatalf("line events = %+v", reporter.events)
	}
}
