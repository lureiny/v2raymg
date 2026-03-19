package log

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"strings"
	"sync"
)

// slogLevelMap 将包内 Level 映射到 slog.Level。
var slogLevelMap = map[Level]slog.Level{
	LevelDebug: slog.LevelDebug,
	LevelInfo:  slog.LevelInfo,
	LevelWarn:  slog.LevelWarn,
	LevelError: slog.LevelError,
}

// buildSlogLogger 根据 Options 构建 slog.Logger 实例。
func buildSlogLogger(o Options) *slog.Logger {
	slogLevel, ok := slogLevelMap[o.Level]
	if !ok {
		slogLevel = slog.LevelInfo
	}

	switch o.Format {
	case FormatJSON:
		handlerOpts := &slog.HandlerOptions{
			Level:     slogLevel,
			AddSource: o.AddSource,
			ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
				if a.Key == slog.SourceKey {
					return slog.Attr{Key: a.Key, Value: slog.AnyValue(shortSource(a))}
				}
				return a
			},
		}
		return slog.New(slog.NewJSONHandler(o.Output, handlerOpts))
	default:
		return slog.New(newV2rayTextHandler(o.Output, slogLevel, o.AddSource))
	}
}

// shortSource extracts a short source path from a slog source attribute.
func shortSource(a slog.Attr) string {
	src, ok := a.Value.Any().(*slog.Source)
	if !ok || src == nil {
		return ""
	}
	file := src.File
	// Keep only "pkg/..." or "cmd/..." suffix
	for _, prefix := range []string{"/pkg/", "/cmd/"} {
		if idx := strings.Index(file, prefix); idx >= 0 {
			file = file[idx+1:]
			break
		}
	}
	return fmt.Sprintf("%s:%d", file, src.Line)
}

// v2rayTextHandler is a minimal custom text handler that outputs:
//
//	2006/01/02 15:04:05 level=INFO source=pkg/foo/bar.go:42 msg="..." key=val
//
// No "time=" prefix — time value is printed directly.
type v2rayTextHandler struct {
	mu        sync.Mutex
	w         io.Writer
	level     slog.Level
	addSource bool
	preAttrs  []slog.Attr // from WithAttrs
	groups    []string    // from WithGroup
}

func newV2rayTextHandler(w io.Writer, level slog.Level, addSource bool) *v2rayTextHandler {
	return &v2rayTextHandler{w: w, level: level, addSource: addSource}
}

func (h *v2rayTextHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *v2rayTextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h2 := *h
	h2.preAttrs = append(append([]slog.Attr{}, h.preAttrs...), attrs...)
	return &h2
}

func (h *v2rayTextHandler) WithGroup(name string) slog.Handler {
	h2 := *h
	h2.groups = append(append([]string{}, h.groups...), name)
	return &h2
}

func (h *v2rayTextHandler) Handle(_ context.Context, r slog.Record) error {
	var buf bytes.Buffer

	// Time — no key prefix
	buf.WriteString(r.Time.Format("2006/01/02 15:04:05"))
	buf.WriteByte(' ')

	// Level
	fmt.Fprintf(&buf, "level=%s", r.Level.String())
	buf.WriteByte(' ')

	// Source
	if h.addSource && r.PC != 0 {
		frames := slog.Source{}
		// Use runtime to resolve PC
		f := r.PC
		_ = f
		// slog.Source from record
		src := &slog.Source{}
		*src = sourceFromPC(r.PC)
		file := src.File
		for _, prefix := range []string{"/pkg/", "/cmd/"} {
			if idx := strings.Index(file, prefix); idx >= 0 {
				file = file[idx+1:]
				break
			}
		}
		_ = frames
		fmt.Fprintf(&buf, "source=%s:%d ", file, src.Line)
	}

	// Message
	fmt.Fprintf(&buf, "msg=%s", quoteIfNeeded(r.Message))

	// Pre-attached attrs (from WithAttrs)
	for _, a := range h.preAttrs {
		buf.WriteByte(' ')
		writeAttr(&buf, h.groups, a)
	}

	// Record attrs
	r.Attrs(func(a slog.Attr) bool {
		buf.WriteByte(' ')
		writeAttr(&buf, h.groups, a)
		return true
	})

	buf.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.w.Write(buf.Bytes())
	return err
}

func writeAttr(buf *bytes.Buffer, groups []string, a slog.Attr) {
	key := a.Key
	for i := len(groups) - 1; i >= 0; i-- {
		key = groups[i] + "." + key
	}
	val := a.Value.Resolve()
	fmt.Fprintf(buf, "%s=%s", key, quoteIfNeeded(val.String()))
}

func quoteIfNeeded(s string) string {
	if strings.ContainsAny(s, " \t\n\"=") {
		return fmt.Sprintf("%q", s)
	}
	return s
}

// sourceFromPC resolves a program counter to a slog.Source.
func sourceFromPC(pc uintptr) slog.Source {
	frames := runtime.CallersFrames([]uintptr{pc})
	f, _ := frames.Next()
	return slog.Source{
		Function: f.Function,
		File:     f.File,
		Line:     f.Line,
	}
}
