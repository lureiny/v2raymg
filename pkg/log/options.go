package log

import (
	"io"
	"os"
)

// Level 日志级别。
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// Format 日志输出格式。
type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
)

// Options 日志配置选项。
type Options struct {
	// Level 最低输出级别，默认 LevelInfo。
	Level Level
	// Format 输出格式：text（人类可读）或 json（结构化）。默认 text。
	Format Format
	// Output 输出目标，默认 os.Stderr。
	Output io.Writer
	// AddSource 是否在日志中包含 caller 文件名和行号，默认 false。
	AddSource bool
}

// defaultOptions 返回默认配置。
func defaultOptions() Options {
	return Options{
		Level:     LevelInfo,
		Format:    FormatText,
		Output:    os.Stderr,
		AddSource: true,
	}
}

// Option 函数式配置项。
type Option func(*Options)

// WithLevel 设置日志级别。
func WithLevel(l Level) Option {
	return func(o *Options) { o.Level = l }
}

// WithFormat 设置输出格式。
func WithFormat(f Format) Option {
	return func(o *Options) { o.Format = f }
}

// WithOutput 设置输出目标。
func WithOutput(w io.Writer) Option {
	return func(o *Options) { o.Output = w }
}

// WithAddSource 设置是否输出 caller 信息。
func WithAddSource(v bool) Option {
	return func(o *Options) { o.AddSource = v }
}
