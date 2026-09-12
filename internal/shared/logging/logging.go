// Package logging は slog の組み立てと、設定値(レベル・形式)の検証を提供する。
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// ParseLevel は設定文字列を slog.Level へ変換する。
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q", s)
	}
}

// ValidateFormat は json / text 以外の形式を拒否する。
func ValidateFormat(s string) error {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "json", "text":
		return nil
	default:
		return fmt.Errorf("unknown log format %q", s)
	}
}

// New は level と format に従った slog.Logger を w に対して構築する。
func New(w io.Writer, level, format string) (*slog.Logger, error) {
	lv, err := ParseLevel(level)
	if err != nil {
		return nil, err
	}
	if formatErr := ValidateFormat(format); formatErr != nil {
		return nil, formatErr
	}
	opts := &slog.HandlerOptions{Level: lv}
	var h slog.Handler
	if strings.EqualFold(strings.TrimSpace(format), "text") {
		h = slog.NewTextHandler(w, opts)
	} else {
		h = slog.NewJSONHandler(w, opts)
	}
	return slog.New(h), nil
}
