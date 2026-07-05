package render

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jedib0t/go-pretty/v6/text"
)

// LevelOK is the house "success" tier — between Info and Warn in slog's scheme.
const LevelOK = slog.Level(2)

// frameworkLogPath matches the shell framework's log (shared file + format) so
// `mu log` sees both producers. MU_LOG_FILE overrides it (tests).
func frameworkLogPath() string {
	if p := os.Getenv("MU_LOG_FILE"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "mayhl_utils", "framework.log")
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
	path := frameworkLogPath()
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
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "scope" {
			scope = a.Value.String()
			return false
		}
		return true
	})
	renderTier(r.Level, r.Message)
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
	fmt.Fprintf(h.file, "[%s] [%-5s] [%s] %s\n",
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

func emit(level slog.Level, msg string) { base().Log(context.Background(), level, msg) }

// Info, OK, Warn, Err are the package-level tiers (default scope "mu").
func Info(msg string) { emit(slog.LevelInfo, msg) }
func OK(msg string)   { emit(LevelOK, msg) }
func Warn(msg string) { emit(slog.LevelWarn, msg) }
func Err(msg string)  { emit(slog.LevelError, msg) }

// Logger is a scope-bound emitter — its records carry a subsystem tag.
type Logger struct{ l *slog.Logger }

// Scoped returns a Logger whose records carry the given subsystem scope.
func Scoped(scope string) *Logger {
	return &Logger{base().With(slog.String("scope", scope))}
}

func (x *Logger) Info(msg string) { x.l.Log(context.Background(), slog.LevelInfo, msg) }
func (x *Logger) OK(msg string)   { x.l.Log(context.Background(), LevelOK, msg) }
func (x *Logger) Warn(msg string) { x.l.Log(context.Background(), slog.LevelWarn, msg) }
func (x *Logger) Err(msg string)  { x.l.Log(context.Background(), slog.LevelError, msg) }
