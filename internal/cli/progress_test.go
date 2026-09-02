package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	progressapi "github.com/course-go-autumn-2026/tripgo-infra/internal/progress"
)

func TestPlainProgressEmitsMilestonesButNotActivityOrANSI(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	cache := t.TempDir()
	session := newProgressSession(&output, false, func() (string, error) { return cache, nil }, "cluster start")
	session.Report(progressapi.Event{Kind: progressapi.Stage, Message: "Creating cluster"})
	session.Report(progressapi.Event{Kind: progressapi.Activity, Message: "\x1b[31mraw password=hunter2\x1b[0m"})
	session.Report(progressapi.Event{Kind: progressapi.Readiness, Message: "control-plane 1/1"})
	session.Report(progressapi.Event{Kind: progressapi.Readiness, Message: "control-plane 1/1"})
	if err := session.finish(nil); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, expected := range []string{"Creating cluster", "control-plane 1/1", "Completed cluster start"} {
		if !strings.Contains(got, expected) {
			t.Errorf("progress output missing %q: %q", expected, got)
		}
	}
	if strings.Contains(got, "raw") || strings.Contains(got, "\x1b") || strings.Count(got, "control-plane 1/1") != 1 {
		t.Fatalf("plain progress output = %q", got)
	}
	entries, err := os.ReadDir(filepath.Join(cache, "tripgoctl", "logs"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("successful operation left logs: %v", entries)
	}
}

func TestFailedProgressRetainsBoundedRedactedTailAndSecureLog(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	cache := t.TempDir()
	session := newProgressSession(&output, false, func() (string, error) { return cache, nil }, "environment start")
	session.Report(progressapi.Event{Kind: progressapi.Stage, Message: "Waiting for workloads"})
	for index := 0; index < 10; index++ {
		session.Report(progressapi.Event{Kind: progressapi.Activity, Message: "line-" + string(rune('a'+index))})
	}
	session.Report(progressapi.Event{Kind: progressapi.Activity, Message: "\x1b[31mtoken=secret-value\x1b[0m"})
	err := session.finish(errors.New("workload failed"))
	if err == nil || session.logPath == "" || !strings.Contains(err.Error(), session.logPath) {
		t.Fatalf("finish error/path = %v, %q", err, session.logPath)
	}
	got := output.String()
	if strings.Contains(got, "line-a") || !strings.Contains(got, "line-j") || strings.Contains(got, "secret-value") || strings.Contains(got, "\x1b") {
		t.Fatalf("failure tail is not bounded/redacted: %q", got)
	}
	if strings.Contains(got, "workload failed") {
		t.Fatalf("operation error was repeated in progress output: %q", got)
	}
	content, readErr := os.ReadFile(session.logPath) // #nosec G304 -- path was created beneath t.TempDir.
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(content), "secret-value") || strings.Contains(string(content), "\x1b") || !strings.Contains(string(content), "[REDACTED]") || !strings.Contains(string(content), "workload failed") {
		t.Fatalf("saved progress log was not sanitized or complete: %q", content)
	}
	info, statErr := os.Stat(session.logPath)
	if statErr != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("progress log mode = %v, error = %v", info.Mode(), statErr)
	}
}

func TestStageIsNotRepeatedInActivityTail(t *testing.T) {
	t.Parallel()
	session := newProgressSession(&bytes.Buffer{}, false, nil, "cluster start")
	session.Report(progressapi.Event{Kind: progressapi.Stage, Message: "Creating cluster"})
	if len(session.tail) != 0 {
		t.Fatalf("stage was added to activity tail: %v", session.tail)
	}
}

func TestFailureLogRetentionIsBounded(t *testing.T) {
	t.Parallel()
	cache := t.TempDir()
	directory := filepath.Join(cache, "tripgoctl", "logs")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < progressLogRetention+3; index++ {
		path := filepath.Join(directory, fmt.Sprintf("progress-old-%02d.log", index))
		if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		when := time.Now().Add(time.Duration(index-progressLogRetention) * time.Minute)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	session := newProgressSession(&bytes.Buffer{}, false, func() (string, error) { return cache, nil }, "cluster start")
	if err := session.finish(errors.New("failed")); err == nil {
		t.Fatal("finish unexpectedly succeeded")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != progressLogRetention {
		t.Fatalf("retained logs = %d, want %d", len(entries), progressLogRetention)
	}
}

func TestInteractiveFinishWaitsForRendererShutdown(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	session := newProgressSession(&output, true, nil, "cluster start")
	session.Report(progressapi.Event{Kind: progressapi.Stage, Message: "Creating cluster"})
	if err := session.finish(nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-session.programDone:
	default:
		t.Fatal("renderer still owns output after finish returned")
	}
	finishedOutput := output.String()
	time.Sleep(100 * time.Millisecond)
	if output.String() != finishedOutput {
		t.Fatal("renderer wrote after finish returned")
	}
}

func TestProgressModelCollapsesSuccessfulOperation(t *testing.T) {
	t.Parallel()
	session := newProgressSession(&bytes.Buffer{}, false, nil, "cluster start")
	session.Report(progressapi.Event{Kind: progressapi.Stage, Message: "Creating cluster"})
	session.mu.Lock()
	session.done = true
	session.mu.Unlock()
	view := newProgressModel(session).View()
	if !strings.Contains(view, "✓ Completed cluster start") || strings.Contains(view, "Creating cluster") || strings.Contains(view, "╭") {
		t.Fatalf("successful progress did not collapse: %q", view)
	}
}

func TestProgressModelCapsVisibleTailForShortTerminal(t *testing.T) {
	t.Parallel()
	session := newProgressSession(&bytes.Buffer{}, false, nil, "cluster start")
	for index := 0; index < 10; index++ {
		session.Report(progressapi.Event{Kind: progressapi.Activity, Message: fmt.Sprintf("entry-%02d %s", index, strings.Repeat("x", 200))})
	}
	model := newProgressModel(session)
	model.width = 40
	model.height = 8 // three activity rows after header/footer overhead
	view := model.View()
	if strings.Count(view, "…") > 3 || strings.Contains(view, "entry-00") || !strings.Contains(view, "entry-09") {
		t.Fatalf("short terminal view was not clipped/tail-bounded:\n%s", view)
	}
}
