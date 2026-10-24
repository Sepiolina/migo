// Package logx is the single logging type used across the service.
//
// A Logger is just "a function that receives one finished line". That keeps
// every package free of a logging library, makes tests trivial (collect the
// lines in a slice) and a nil Logger is safe to call, so a struct that was
// built without one never needs "if log == nil" guards.
package logx

import (
	"fmt"
	"log"
	"os"
)

// Logger receives one log line (no trailing newline).
type Logger func(line string)

// Nop discards everything.
var Nop Logger = func(string) {}

// Stdout logs to standard output with the standard timestamp prefix.
func Stdout() Logger {
	l := log.New(os.Stdout, "", log.LstdFlags)
	return func(line string) { l.Println(line) }
}

// Print logs a ready-made line. Safe on a nil Logger.
func (l Logger) Print(line string) {
	if l != nil {
		l(line)
	}
}

// Printf logs a formatted line. Safe on a nil Logger.
func (l Logger) Printf(format string, args ...any) {
	if l != nil {
		l(fmt.Sprintf(format, args...))
	}
}
