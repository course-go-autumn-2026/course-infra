package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	progressapi "github.com/course-go-autumn-2026/tripgo-infra/internal/progress"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/redact"
)

const (
	progressTailRows     = 6
	progressLineBytes    = 2048
	progressLogMaxBytes  = 2 * 1024 * 1024
	progressLogRetention = 10
)

type progressSession struct {
	mu           sync.Mutex
	output       io.Writer
	cacheBase    func() (string, error)
	title        string
	started      time.Time
	interactive  bool
	stage        string
	lastStage    string
	lastReady    string
	tail         []string
	log          strings.Builder
	logFull      bool
	done         bool
	operationErr error
	logPath      string
	writeErr     error
	programDone  chan struct{}
	cancelUI     context.CancelFunc
}

func newProgressSession(output io.Writer, interactive bool, cacheBase func() (string, error), title string) *progressSession {
	session := &progressSession{
		output: output, cacheBase: cacheBase, title: title, started: time.Now(),
		interactive: interactive, tail: make([]string, 0, progressTailRows),
	}
	if interactive {
		session.startInteractive()
	}
	return session
}

func (s *progressSession) context(ctx context.Context) context.Context {
	return progressapi.WithReporter(ctx, s)
}

func (s *progressSession) Report(event progressapi.Event) {
	lines := safeProgressLines(event.Message)
	for _, line := range lines {
		s.reportLine(event.Kind, line)
	}
}

func (s *progressSession) reportLine(kind progressapi.Kind, line string) {
	if line == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return
	}
	if kind == progressapi.Stage {
		if line == s.lastStage {
			return
		}
		s.stage, s.lastStage = line, line
	}
	if kind == progressapi.Readiness {
		if line == s.lastReady {
			return
		}
		s.lastReady = line
	}
	s.appendLogLocked(kind, line)
	if kind != progressapi.Stage {
		s.tail = append(s.tail, line)
		if len(s.tail) > progressTailRows {
			s.tail = append(s.tail[:0], s.tail[len(s.tail)-progressTailRows:]...)
		}
	}
	if !s.interactive && (kind == progressapi.Stage || kind == progressapi.Readiness || kind == progressapi.Warning) {
		s.writeLocked(fmt.Sprintf("[%s] %s\n", time.Since(s.started).Round(time.Second), line))
	}
}

func (s *progressSession) appendLogLocked(kind progressapi.Kind, line string) {
	if s.logFull {
		return
	}
	prefix := map[progressapi.Kind]string{
		progressapi.Stage: "stage", progressapi.Activity: "activity",
		progressapi.Readiness: "readiness", progressapi.Warning: "warning",
	}[kind]
	entry := fmt.Sprintf("[%s] %-9s %s\n", time.Since(s.started).Round(time.Millisecond), prefix, line)
	if s.log.Len()+len(entry) > progressLogMaxBytes {
		marker := "[progress log truncated at 2 MiB]\n"
		remaining := progressLogMaxBytes - s.log.Len()
		if remaining >= len(marker) {
			s.log.WriteString(marker)
		}
		s.logFull = true
		return
	}
	s.log.WriteString(entry)
}

func (s *progressSession) finish(operationErr error) error {
	var failureLines []string
	if operationErr != nil {
		failureLines = safeProgressLines("Failure: " + operationErr.Error())
	}

	s.mu.Lock()
	if operationErr != nil {
		for _, line := range failureLines {
			s.appendLogLocked(progressapi.Warning, line)
		}
		s.logPath = s.saveFailureLogLocked()
	}
	s.operationErr = operationErr
	s.done = true
	interactive := s.interactive
	done := s.programDone
	cancel := s.cancelUI
	if !interactive {
		s.renderPlainFinishLocked()
	}
	logPath := s.logPath
	s.mu.Unlock()

	if interactive {
		// Do not relinquish stderr ownership until Bubble Tea has completed its
		// final render. The lifecycle operation (including failure cleanup) has
		// already returned, so this wait cannot delay resource cleanup.
		<-done
		cancel()
	}
	if operationErr != nil {
		return &progressOperationError{cause: operationErr, logPath: logPath}
	}
	return nil
}

type progressOperationError struct {
	cause   error
	logPath string
}

func (e *progressOperationError) Error() string {
	message := redact.TerminalText(e.cause.Error(), 16*1024)
	if e.logPath != "" {
		return fmt.Sprintf("%s (full progress log: %s)", message, e.logPath)
	}
	return message
}

func (e *progressOperationError) Unwrap() error { return e.cause }

func (s *progressSession) saveFailureLogLocked() string {
	if s.cacheBase == nil {
		return ""
	}
	base, err := s.cacheBase()
	if err != nil {
		return ""
	}
	directory := filepath.Join(base, "tripgoctl", "logs")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return ""
	}
	pruneFailureLogs(directory, progressLogRetention-1)
	file, err := os.CreateTemp(directory, "progress-*.log")
	if err != nil {
		return ""
	}
	path := file.Name()
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return ""
	}
	if _, err := io.WriteString(file, s.log.String()); err != nil {
		return ""
	}
	if err := file.Sync(); err != nil {
		return ""
	}
	if err := file.Close(); err != nil {
		return ""
	}
	ok = true
	return path
}

func pruneFailureLogs(directory string, keep int) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	type logEntry struct {
		name    string
		modTime time.Time
	}
	logs := make([]logEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "progress-") || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr == nil {
			logs = append(logs, logEntry{name: entry.Name(), modTime: info.ModTime()})
		}
	}
	sort.Slice(logs, func(i, j int) bool { return logs[i].modTime.After(logs[j].modTime) })
	if keep < 0 {
		keep = 0
	}
	for _, entry := range logs[min(keep, len(logs)):] {
		_ = os.Remove(filepath.Join(directory, entry.name))
	}
}

func (s *progressSession) renderPlainFinishLocked() {
	elapsed := time.Since(s.started).Round(time.Second)
	if s.operationErr == nil {
		s.writeLocked(fmt.Sprintf("Completed %s in %s\n", s.title, elapsed))
		return
	}
	s.writeLocked(fmt.Sprintf("Failed %s at %s after %s\n", s.title, fallbackStage(s.stage), elapsed))
	if len(s.tail) > 0 {
		s.writeLocked("Recent activity:\n")
		for _, line := range s.tail {
			s.writeLocked("  " + line + "\n")
		}
	}
}

func (s *progressSession) writeLocked(value string) {
	if s.writeErr != nil {
		return
	}
	_, s.writeErr = io.WriteString(s.output, value)
}

func (s *progressSession) startInteractive() {
	uiContext, cancel := context.WithCancel(context.Background())
	s.cancelUI = cancel
	s.programDone = make(chan struct{})
	model := newProgressModel(s)
	program := tea.NewProgram(model,
		tea.WithContext(uiContext),
		tea.WithInput(nil),
		tea.WithOutput(s.output),
		tea.WithoutSignalHandler(),
	)
	go func() {
		defer close(s.programDone)
		_, _ = program.Run()
	}()
}

type progressSnapshot struct {
	title   string
	stage   string
	tail    []string
	elapsed time.Duration
	done    bool
	failed  bool
}

func (s *progressSession) snapshot() progressSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return progressSnapshot{
		title: s.title, stage: s.stage, tail: append([]string(nil), s.tail...),
		elapsed: time.Since(s.started), done: s.done, failed: s.operationErr != nil,
	}
}

type refreshProgressMsg time.Time

type progressModel struct {
	session *progressSession
	spinner spinner.Model
	width   int
	height  int
}

func newProgressModel(session *progressSession) progressModel {
	indicator := spinner.New()
	indicator.Spinner = spinner.Dot
	return progressModel{session: session, spinner: indicator, width: 80, height: 24}
}

func (m progressModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, refreshProgress())
}

func refreshProgress() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(now time.Time) tea.Msg { return refreshProgressMsg(now) })
}

func (m progressModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height
	case refreshProgressMsg:
		if m.session.snapshot().done {
			return m, tea.Quit
		}
		return m, refreshProgress()
	case spinner.TickMsg:
		var command tea.Cmd
		m.spinner, command = m.spinner.Update(message)
		return m, command
	}
	return m, nil
}

func (m progressModel) View() string {
	snapshot := m.session.snapshot()
	width := m.width
	if width <= 0 {
		width = 80
	}
	if width > 100 {
		width = 100
	}
	if width < 24 {
		width = 24
	}
	elapsed := snapshot.elapsed.Round(time.Second)
	if snapshot.done && !snapshot.failed {
		return fmt.Sprintf("✓ Completed %s in %s\n", snapshot.title, elapsed)
	}
	if snapshot.done {
		return m.failureView(snapshot, width, elapsed)
	}
	rows := progressTailRows
	if available := m.height - 5; available < rows {
		rows = available
	}
	if rows < 1 {
		rows = 1
	}
	tail := snapshot.tail
	if len(tail) > rows {
		tail = tail[len(tail)-rows:]
	}
	innerWidth := width - 4
	lines := make([]string, 0, rows)
	for _, line := range tail {
		lines = append(lines, clipDisplayWidth(line, innerWidth))
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	header := fmt.Sprintf("%s %s · %s", m.spinner.View(), snapshot.title, elapsed)
	stage := fallbackStage(snapshot.stage)
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Width(width - 2).Render(strings.Join(lines, "\n"))
	return clipDisplayWidth(header, width) + "\n" + clipDisplayWidth(stage, width) + "\n" + box + "\nCtrl+C to cancel\n"
}

func (m progressModel) failureView(snapshot progressSnapshot, width int, elapsed time.Duration) string {
	lines := make([]string, 0, len(snapshot.tail))
	for _, line := range snapshot.tail {
		lines = append(lines, clipDisplayWidth(line, width-4))
	}
	box := ""
	if len(lines) > 0 {
		box = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Width(width-2).Render(strings.Join(lines, "\n")) + "\n"
	}
	return fmt.Sprintf("✗ Failed %s at %s after %s\n%s", snapshot.title, fallbackStage(snapshot.stage), elapsed, box)
}

func safeProgressLines(message string) []string {
	message = redact.TerminalText(message, progressLineBytes*progressTailRows)
	message = strings.ReplaceAll(message, "\r", "\n")
	parts := strings.Split(message, "\n")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(redact.TerminalText(part, progressLineBytes))
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func clipDisplayWidth(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	var result strings.Builder
	for _, character := range value {
		candidate := result.String() + string(character)
		if lipgloss.Width(candidate) > width-1 {
			break
		}
		result.WriteRune(character)
	}
	return result.String() + "…"
}

func fallbackStage(stage string) string {
	if stage == "" {
		return "starting"
	}
	return stage
}

var _ progressapi.Reporter = (*progressSession)(nil)
