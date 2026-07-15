package render

// Level is the global output verbosity, set once from the root command's -q/-v flags. It gates
// the ancillary tiers: Info and Detail go quiet at -q, and Verbose lines appear only at -v.
// OK/Warn/Err always print — a result or a failure is never something a level should swallow.
type Level int

const (
	LevelQuiet   Level = -1 // -q: results, warnings, and errors only
	LevelNormal  Level = 0  // default
	LevelVerbose Level = 1  // -v: everything, including the per-command extras
)

// Verbosity is the process-wide level. Set at flag-parse time (see the root -q/-v flags), so it
// is already in place before any command's RunE — independent of PersistentPreRun chains, which
// Cobra runs only the deepest of.
var Verbosity = LevelNormal

// IsVerbose reports whether -v is in effect — commands read it to decide whether to emit their
// own verbose extras (cp's per-file lines, doctor's per-check detail, the job flow's chatter).
func IsVerbose() bool { return Verbosity >= LevelVerbose }

// IsQuiet reports whether -q is in effect.
func IsQuiet() bool { return Verbosity <= LevelQuiet }

// Verbose prints a gray Detail line ONLY under -v: for output that helps diagnosis but is noise
// by default (a submit's resolved script and applied directives, a tunnel's mode). The daily
// path stays terse; `mu <cmd> -v` reveals it.
func Verbose(msg string) {
	if IsVerbose() {
		Detail(msg)
	}
}
