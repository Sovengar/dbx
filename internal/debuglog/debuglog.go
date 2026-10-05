// Package debuglog is the one place dbx writes its debug logs.
//
// # Why this exists
//
// Nine copies of the same function used to open a file under /tmp, append one line and
// close it — six files, three spellings of the OpenFile flags, and two different error
// styles:
//
//	f, err := os.OpenFile("/tmp/dbx_app_debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
//	if err != nil { return }
//
// versus:
//
//	f, _ := os.OpenFile("/tmp/dbx_mode_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
//	if f != nil { ... }
//
// The flags are equivalent, so that part is only untidy. The error style is not: four of the
// nine have an arm that reports a failure to open the log and gives up, and that arm cannot
// be reached from a test, because it only fires when /tmp is unwritable. Four uncovered
// statements in four packages, all of them the same statement, none of them testable.
//
// So the logic lives here, once, with the PATH AS AN ARGUMENT. A caller that cannot be
// tested can now be tested, by naming a path that cannot be opened.
//
// # Why the logs stay
//
// These files are how this codebase is debugged, and the project's own rules forbid
// removing them. So they are not going away; what goes away is the duplication, and with it
// the drift.
package debuglog

import (
	"fmt"
	"os"
)

// DefaultDir is where the logs live. It is a variable rather than a constant only so a test
// can redirect it — production never does.
var DefaultDir = "/tmp"

// Path is the log file for a component: DefaultDir + "/dbx_" + component + "_debug.log".
//
// The naming is the project's convention and it is load-bearing in a way that is easy to
// forget: an investigation starts with `ls /tmp/dbx_*_debug.log`, so a component that
// invents its own filename disappears from that listing.
func Path(component string) string {
	return DefaultDir + "/dbx_" + component + "_debug.log"
}

// Write appends one line to the component's log, tagged with prefix.
//
// It never returns an error and never panics. Debug logging runs inside key handlers and
// render paths, and a logging call that could fail a render is worse than a lost log line —
// so a log that cannot be written is a log that is not written, and nothing else.
//
// The one thing worth knowing: this opens, appends and closes on every call. That is the
// behaviour the nine copies had, kept because it is what makes a log readable while the
// program is still running.
func Write(component, prefix, format string, args ...any) {
	appendLine(Path(component), prefix, format, args...)
}

// appendLine is Write with the path given, which is what makes the failure case testable
// without touching DefaultDir and without needing a filesystem that misbehaves.
func appendLine(path, prefix, format string, args ...any) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	line := format
	if prefix != "" {
		line = prefix + ": " + format
	}
	_, _ = fmt.Fprintf(f, line+"\n", args...)
}

// Append is Write for a caller that already knows its path. It exists so the three
// components that open a log INLINE, rather than through a wrapper, can share the logic
// without each keeping a copy of it.
func Append(path, prefix, format string, args ...any) {
	appendLine(path, prefix, format, args...)
}
