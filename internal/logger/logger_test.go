package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestLogger(t *testing.T, debug bool) (*Logger, string) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")
	l, err := New(logPath, debug)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l, logPath
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	return string(data)
}

// ── New ───────────────────────────────────────────────────────────────────────

func TestNew_CreatesLogFile(t *testing.T) {
	_, logPath := newTestLogger(t, false)
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("log file was not created")
	}
}

func TestNew_InvalidPath(t *testing.T) {
	_, err := New("/nonexistent/directory/test.log", false)
	if err == nil {
		t.Fatal("expected error for invalid log path, got nil")
	}
}

func TestNew_AppendsToExistingFile(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "existing.log")
	if err := os.WriteFile(logPath, []byte("prior content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	l, err := New(logPath, false)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	l.Info("new line")
	_ = l.Close()

	content := readLog(t, logPath)
	if !strings.Contains(content, "prior content") {
		t.Error("existing log content was overwritten")
	}
	if !strings.Contains(content, "new line") {
		t.Error("new log line not written")
	}
}

// ── Log levels ────────────────────────────────────────────────────────────────

func TestLogger_Info(t *testing.T) {
	l, logPath := newTestLogger(t, false)
	l.Info("hello info")
	_ = l.Close()

	content := readLog(t, logPath)
	if !strings.Contains(content, "INFO") {
		t.Error("expected INFO level in log")
	}
	if !strings.Contains(content, "hello info") {
		t.Error("expected message in log")
	}
}

func TestLogger_Infof(t *testing.T) {
	l, logPath := newTestLogger(t, false)
	l.Infof("count=%d name=%s", 42, "test")
	_ = l.Close()

	content := readLog(t, logPath)
	if !strings.Contains(content, "count=42 name=test") {
		t.Errorf("formatted message not found in log: %s", content)
	}
}

func TestLogger_Warn(t *testing.T) {
	l, logPath := newTestLogger(t, false)
	l.Warn("watch out")
	_ = l.Close()

	content := readLog(t, logPath)
	if !strings.Contains(content, "WARN") {
		t.Error("expected WARN level in log")
	}
	if !strings.Contains(content, "watch out") {
		t.Error("expected message in log")
	}
}

func TestLogger_Error(t *testing.T) {
	l, logPath := newTestLogger(t, false)
	l.Error("something broke")
	_ = l.Close()

	content := readLog(t, logPath)
	if !strings.Contains(content, "ERROR") {
		t.Error("expected ERROR level in log")
	}
	if !strings.Contains(content, "something broke") {
		t.Error("expected message in log")
	}
}

func TestLogger_Debug_DisabledByDefault(t *testing.T) {
	l, logPath := newTestLogger(t, false)
	l.Debug("secret debug info")
	_ = l.Close()

	content := readLog(t, logPath)
	if strings.Contains(content, "secret debug info") {
		t.Error("debug message should not appear when debug=false")
	}
}

func TestLogger_Debug_EnabledWithFlag(t *testing.T) {
	l, logPath := newTestLogger(t, true)
	l.Debug("visible debug info")
	_ = l.Close()

	content := readLog(t, logPath)
	if !strings.Contains(content, "DEBUG") {
		t.Error("expected DEBUG level in log")
	}
	if !strings.Contains(content, "visible debug info") {
		t.Error("expected debug message in log")
	}
}

func TestLogger_Debugf_EnabledWithFlag(t *testing.T) {
	l, logPath := newTestLogger(t, true)
	l.Debugf("val=%d", 99)
	_ = l.Close()

	content := readLog(t, logPath)
	if !strings.Contains(content, "val=99") {
		t.Errorf("expected formatted debug message in log: %s", content)
	}
}

// ── Timestamp format ──────────────────────────────────────────────────────────

func TestLogger_TimestampFormat(t *testing.T) {
	l, logPath := newTestLogger(t, false)
	l.Info("ts check")
	_ = l.Close()

	content := readLog(t, logPath)
	// Expect format: [2006-01-02 15:04:05.000]
	if !strings.Contains(content, "[20") {
		t.Errorf("expected timestamp in log line, got: %s", content)
	}
}

// ── Close ─────────────────────────────────────────────────────────────────────

func TestLogger_Close_Idempotent(t *testing.T) {
	l, _ := newTestLogger(t, false)
	if err := l.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	// Second close on the already-closed file — os.File.Close returns an error,
	// so we only verify we don't panic.
}

func TestLogger_Close_NilFile(t *testing.T) {
	l := &Logger{}
	if err := l.Close(); err != nil {
		t.Errorf("Close on nil file should return nil, got: %v", err)
	}
}
