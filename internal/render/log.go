package render

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jedib0t/go-pretty/v6/text"
)

// LevelOK is the house "success" tier — between Info and Warn in slog's scheme.
const LevelOK = slog.Level(2)

// eventLogPath is the curated event log (`mu`'s Step/Event/`mu log write`), read by
// `mu log`. Distinct from the shell framework's framework.log (debug chatter).
// MU_LOG_FILE overrides (config.toml [log].file wires it; tests set it directly).
func eventLogPath() string {
	if p := os.Getenv("MU_LOG_FILE"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "mayhl_utils", "events.log")
}

// houseHandler is the single log sink: renders house-style tiers to stderr and
// appends every record to framework.log as `[ts] [level] [scope] msg`. Future
// format/field/sink changes live here, never at call sites. Logging failures are
// swallowed — they must never break the tool.
type houseHandler struct {
	scope string
	mu    *sync.Mutex
	file  *os.File // nil if framework.log can't be opened
}

func newHouseHandler(scope string) *houseHandler {
	h := &houseHandler{scope: scope, mu: &sync.Mutex{}}
	path := eventLogPath()
	if os.MkdirAll(filepath.Dir(path), 0o755) == nil {
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			h.file = f
		}
	}
	return h
}

func (h *houseHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *houseHandler) Handle(_ context.Context, r slog.Record) error {
	scope := h.scope
	quiet := false // log-only: skip the terminal render (command owns its own UX)
	r.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "scope":
			scope = a.Value.String()
		case "quiet":
			quiet = a.Value.Bool()
		}
		return true
	})
	if !quiet {
		renderTier(r.Level, r.Message)
	}
	h.append(r.Time, r.Level, scope, r.Message)
	return nil
}

// WithAttrs binds a scope (the only attr we serialize today); the new handler
// shares the file + mutex (pointers) so all scopes append to one log.
func (h *houseHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	nh := *h
	for _, a := range attrs {
		if a.Key == "scope" {
			nh.scope = a.Value.String()
		}
	}
	return &nh
}

func (h *houseHandler) WithGroup(string) slog.Handler { return h }

func (h *houseHandler) append(t time.Time, level slog.Level, scope, msg string) {
	if h.file == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	_, _ = fmt.Fprintf(h.file, "[%s] [%-5s] [%s] %s\n",
		t.Format("2006-01-02T15:04:05"), levelName(level), scope, msg)
}

func levelName(l slog.Level) string {
	switch l {
	case slog.LevelInfo:
		return "INFO"
	case LevelOK:
		return "OK"
	case slog.LevelWarn:
		return "WARN"
	case slog.LevelError:
		return "ERROR"
	default:
		return l.String()
	}
}

// renderTier prints one house-style status line to stderr (glyph + color),
// preserving the pre-slog mu_log appearance.
func renderTier(level slog.Level, msg string) {
	switch level {
	case LevelOK:
		logLine("✓", "[OK]", text.Colors{text.FgGreen, text.Bold}, msg)
	case slog.LevelWarn:
		logLine("!", "[WARN]", text.Colors{text.FgYellow, text.Bold}, msg)
	case slog.LevelError:
		logLine("✗", "[ERROR]", text.Colors{text.FgRed, text.Bold}, msg)
	default: // Info and anything unmapped
		logLine("→", "[INFO]", text.Colors{text.FgCyan}, msg)
	}
}

var (
	rootLogger *slog.Logger
	loggerOnce sync.Once
)

func base() *slog.Logger {
	loggerOnce.Do(func() { rootLogger = slog.New(newHouseHandler("mu")) })
	return rootLogger
}

// ResetLoggerForTest re-inits the sink so a test can repoint MU_LOG_FILE.
func ResetLoggerForTest() {
	loggerOnce = sync.Once{}
	rootLogger = nil
}

// Info, OK, Warn, Err print a house-style tier line to stderr — terminal only,
// NOT recorded. They're ephemeral UI messages; the curated event log is fed only
// by the scoped/event paths (Scoped, Step, Event*, `mu log write`). Rule of thumb:
// scoped ⇒ logged event; unscoped ⇒ ephemeral message.
func Info(msg string) { renderTier(slog.LevelInfo, msg) }
func OK(msg string)   { renderTier(LevelOK, msg) }
func Warn(msg string) { renderTier(slog.LevelWarn, msg) }
func Err(msg string)  { renderTier(slog.LevelError, msg) }

// Logger is a scope-bound emitter — its records are recorded in the event log.
type Logger struct{ l *slog.Logger }

// Scoped returns a Logger whose records carry the given subsystem scope.
func Scoped(scope string) *Logger {
	return &Logger{base().With(slog.String("scope", scope))}
}

func (x *Logger) Info(msg string) { x.l.Log(context.Background(), slog.LevelInfo, msg) }
func (x *Logger) OK(msg string)   { x.l.Log(context.Background(), LevelOK, msg) }
func (x *Logger) Warn(msg string) { x.l.Log(context.Background(), slog.LevelWarn, msg) }
func (x *Logger) Err(msg string)  { x.l.Log(context.Background(), slog.LevelError, msg) }

// EventLogPath is the curated event-log file that `mu log` reads.
func EventLogPath() string { return eventLogPath() }

// event appends a log-only record (quiet — no terminal render) for commands that
// print their own richer UX (progress bars, summaries) but still want a durable entry.
func event(level slog.Level, scope, msg string) {
	base().LogAttrs(context.Background(), level, msg,
		slog.String("scope", scope), slog.Bool("quiet", true))
}

// EventInfo/OK/Warn/Err append a log-only event under the given scope.
func EventInfo(scope, msg string) { event(slog.LevelInfo, scope, msg) }
func EventOK(scope, msg string)   { event(LevelOK, scope, msg) }
func EventWarn(scope, msg string) { event(slog.LevelWarn, scope, msg) }
func EventErr(scope, msg string)  { event(slog.LevelError, scope, msg) }

// FormatEntry returns a colored one-line rendering of a log entry for readers like
// `mu log`: "<ts> <glyph> [scope] msg", styled by level (reuses the house palette).
func FormatEntry(levelName, scope, ts, msg string) string {
	var col text.Colors
	var g string
	switch strings.ToUpper(levelName) {
	case "OK":
		col, g = text.Colors{text.FgGreen, text.Bold}, glyph("✓", "[OK]")
	case "WARN", "WARNING":
		col, g = text.Colors{text.FgYellow, text.Bold}, glyph("!", "[WARN]")
	case "ERROR", "ERR":
		col, g = text.Colors{text.FgRed, text.Bold}, glyph("✗", "[ERROR]")
	default:
		col, g = text.Colors{text.FgCyan}, glyph("→", "[INFO]")
	}
	tag, tsOut, scopeOut := g, ts, "["+scope+"]"
	if !colorOff() {
		dim := text.Colors{text.FgHiBlack}
		tag, tsOut, scopeOut = col.Sprint(g), dim.Sprint(ts), dim.Sprint("["+scope+"]")
	}
	if scope != "" {
		return fmt.Sprintf("%s %s %s %s", tsOut, tag, scopeOut, msg)
	}
	return fmt.Sprintf("%s %s %s", tsOut, tag, msg)
}

// WriteEvent renders + logs an event chosen by a level string (for `mu log write`).
func WriteEvent(scope, levelStr, msg string) {
	l := Scoped(scope)
	switch strings.ToUpper(strings.TrimSpace(levelStr)) {
	case "OK":
		l.OK(msg)
	case "WARN", "WARNING":
		l.Warn(msg)
	case "ERR", "ERROR":
		l.Err(msg)
	default:
		l.Info(msg)
	}
}
