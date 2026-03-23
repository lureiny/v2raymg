package log_test

import (
	"bytes"
	"strings"
	"testing"

	pkglog "github.com/lureiny/v2raymg/pkg/log"
)

func TestNew_DefaultConfig(t *testing.T) {
	l := pkglog.New()
	if l == nil {
		t.Fatal("New() returned nil")
	}
}

func TestWithLevel(t *testing.T) {
	var buf bytes.Buffer
	l := pkglog.New(pkglog.WithLevel(pkglog.LevelError), pkglog.WithOutput(&buf))
	l.Info("should not appear")
	if buf.Len() != 0 {
		t.Error("expected no output for INFO when level=ERROR")
	}
	l.Error("visible")
	if buf.Len() == 0 {
		t.Error("expected output for ERROR when level=ERROR")
	}
}

func TestWithFormat_JSON(t *testing.T) {
	var buf bytes.Buffer
	l := pkglog.New(pkglog.WithFormat(pkglog.FormatJSON), pkglog.WithOutput(&buf))
	l.Info("test message")
	if !strings.Contains(buf.String(), `"msg"`) {
		t.Errorf("expected JSON output, got: %s", buf.String())
	}
}

func TestWithFormat_Text(t *testing.T) {
	var buf bytes.Buffer
	l := pkglog.New(pkglog.WithFormat(pkglog.FormatText), pkglog.WithOutput(&buf))
	l.Info("test message")
	if !strings.Contains(buf.String(), "msg=") {
		t.Errorf("expected text output, got: %s", buf.String())
	}
}

func TestWithOutput(t *testing.T) {
	var buf bytes.Buffer
	l := pkglog.New(pkglog.WithOutput(&buf))
	l.Info("hello")
	if buf.Len() == 0 {
		t.Error("expected output written to custom writer")
	}
}

func TestWithAddSource(t *testing.T) {
	var buf bytes.Buffer
	l := pkglog.New(pkglog.WithAddSource(true), pkglog.WithOutput(&buf))
	l.Info("source test")
	if !strings.Contains(buf.String(), "source=") {
		t.Errorf("expected source info in output, got: %s", buf.String())
	}
}

func TestSetDefault_Default(t *testing.T) {
	var buf bytes.Buffer
	custom := pkglog.New(pkglog.WithOutput(&buf))
	pkglog.SetDefault(custom)
	defer pkglog.SetDefault(pkglog.New()) // restore

	if pkglog.Default() != custom {
		t.Error("Default() should return the logger set by SetDefault()")
	}
}

func TestGlobalFunctions_NoPanic(t *testing.T) {
	var buf bytes.Buffer
	pkglog.SetDefault(pkglog.New(pkglog.WithOutput(&buf)))
	defer pkglog.SetDefault(pkglog.New())

	pkglog.Debug("debug")
	pkglog.Info("info")
	pkglog.Warn("warn")
	pkglog.Error("error")
}

func TestLogger_With(t *testing.T) {
	var buf bytes.Buffer
	l := pkglog.New(pkglog.WithOutput(&buf))
	l2 := l.With("key", "value")
	if l2 == nil {
		t.Fatal("With() returned nil")
	}
	l2.Info("with test")
	if !strings.Contains(buf.String(), "key=value") {
		t.Errorf("expected key=value in output, got: %s", buf.String())
	}
}

func TestGlobal_With(t *testing.T) {
	var buf bytes.Buffer
	pkglog.SetDefault(pkglog.New(pkglog.WithOutput(&buf)))
	defer pkglog.SetDefault(pkglog.New())

	l := pkglog.With("component", "test")
	if l == nil {
		t.Fatal("With() returned nil")
	}
}

func TestDefaultPrefix(t *testing.T) {
	var buf bytes.Buffer
	l := pkglog.New(pkglog.WithOutput(&buf))
	l.Info("hello")
	out := buf.String()
	if !strings.HasPrefix(out, "[v2raymg] ") {
		t.Errorf("expected output to start with [v2raymg], got: %s", out)
	}
}

func TestWithPrefix_Custom(t *testing.T) {
	var buf bytes.Buffer
	l := pkglog.New(pkglog.WithOutput(&buf), pkglog.WithPrefix("myapp"))
	l.Info("hello")
	out := buf.String()
	if !strings.HasPrefix(out, "[myapp] ") {
		t.Errorf("expected output to start with [myapp], got: %s", out)
	}
}

func TestWithPrefix_Empty(t *testing.T) {
	var buf bytes.Buffer
	l := pkglog.New(pkglog.WithOutput(&buf), pkglog.WithPrefix(""))
	l.Info("hello")
	out := buf.String()
	if strings.HasPrefix(out, "[") {
		t.Errorf("expected no prefix, got: %s", out)
	}
}

func TestPrefixWriter_Lines(t *testing.T) {
	var buf bytes.Buffer
	pw := pkglog.NewPrefixWriter(&buf, "xray")
	_, _ = pw.Write([]byte("line one\nline two\n"))
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), out)
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "[xray] ") {
			t.Errorf("expected line to start with [xray], got: %q", line)
		}
	}
}

func TestPrefixWriter_Flush(t *testing.T) {
	var buf bytes.Buffer
	pw := pkglog.NewPrefixWriter(&buf, "snell")
	_, _ = pw.Write([]byte("incomplete line"))
	if buf.Len() != 0 {
		t.Error("expected no output before Flush")
	}
	_ = pw.Flush()
	if !strings.Contains(buf.String(), "[snell] incomplete line") {
		t.Errorf("expected flushed output with prefix, got: %q", buf.String())
	}
}
