package observability

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type prettyHandler struct {
	opts  slog.HandlerOptions
	color bool

	mu  *sync.Mutex
	out io.Writer

	groups []string
	attrs  []slog.Attr
}

const (
	ansiReset  = "\033[0m"
	ansiDim    = "\033[2m"
	ansiBold   = "\033[1m"
	ansiRed    = "\033[31m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
	ansiGray   = "\033[90m"
)

func newPrettyHandler(out io.Writer, opts slog.HandlerOptions, color bool) *prettyHandler {
	return &prettyHandler{
		opts:  opts,
		color: color,
		mu:    &sync.Mutex{},
		out:   out,
	}
}

func (h *prettyHandler) Enabled(_ context.Context, level slog.Level) bool {
	minimum := slog.LevelInfo
	if h.opts.Level != nil {
		minimum = h.opts.Level.Level()
	}
	return level >= minimum
}

func (h *prettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	clone := *h

	clone.attrs = make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	clone.attrs = append(clone.attrs, h.attrs...)
	clone.attrs = append(clone.attrs, attrs...)
	return &clone
}

func (h *prettyHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.groups = make([]string, 0, len(h.groups)+1)
	clone.groups = append(clone.groups, h.groups...)
	clone.groups = append(clone.groups, name)
	return &clone
}

func (h *prettyHandler) Handle(_ context.Context, record slog.Record) error {
	var line strings.Builder

	stamp := record.Time.Format("15:04:05.000")
	line.WriteString(h.paint(ansiGray, stamp))
	line.WriteByte(' ')

	line.WriteString(h.paint(levelColor(record.Level), levelLabel(record.Level)))
	line.WriteByte(' ')

	line.WriteString(h.paint(ansiBold, record.Message))

	for _, attr := range h.attrs {
		h.appendAttr(&line, h.groups, attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		h.appendAttr(&line, h.groups, attr)
		return true
	})

	line.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.out, line.String())
	return err
}

func (h *prettyHandler) appendAttr(line *strings.Builder, groups []string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return
	}

	if attr.Value.Kind() == slog.KindGroup {
		members := attr.Value.Group()
		if len(members) == 0 {
			return
		}
		nested := groups
		if attr.Key != "" {
			nested = append(append([]string{}, groups...), attr.Key)
		}
		for _, member := range members {
			h.appendAttr(line, nested, member)
		}
		return
	}

	key := attr.Key
	if len(groups) > 0 {
		key = strings.Join(groups, ".") + "." + key
	}

	line.WriteByte(' ')
	line.WriteString(h.paint(ansiDim, key+"="))
	line.WriteString(formatValue(attr.Value))
}

func formatValue(v slog.Value) string {
	switch v.Kind() {
	case slog.KindDuration:
		return strconv.FormatFloat(float64(v.Duration())/float64(time.Millisecond), 'f', 2, 64) + "ms"
	case slog.KindTime:
		return v.Time().Format(time.RFC3339)
	case slog.KindString:
		s := v.String()
		if s == "" {
			return `""`
		}
		if strings.ContainsAny(s, " \t\n\"") {
			return strconv.Quote(s)
		}
		return s
	default:
		return fmt.Sprint(v.Any())
	}
}

func (h *prettyHandler) paint(color, text string) string {
	if !h.color {
		return text
	}
	return color + text + ansiReset
}

func levelLabel(level slog.Level) string {
	switch {
	case level < slog.LevelInfo:
		return "DBG"
	case level < slog.LevelWarn:
		return "INF"
	case level < slog.LevelError:
		return "WRN"
	default:
		return "ERR"
	}
}

func levelColor(level slog.Level) string {
	switch {
	case level < slog.LevelInfo:
		return ansiGray
	case level < slog.LevelWarn:
		return ansiCyan
	case level < slog.LevelError:
		return ansiYellow
	default:
		return ansiRed
	}
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
