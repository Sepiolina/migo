package workflow

import "fmt"

// Config provides the values exposed by the config(key) expression helper.
type Config interface {
	Value(key string) any
}

// Values provides config expression values from a map.
type Values map[string]any

// Value returns the value associated with key, or nil if it is absent.
func (v Values) Value(key string) any {
	return v[key]
}

// Logger receives one log line without a trailing newline.
type Logger func(line string)

// Print logs a line if the logger is set.
func (l Logger) Print(line string) {
	if l != nil {
		l(line)
	}
}

// Printf logs a formatted line if the logger is set.
func (l Logger) Printf(format string, args ...any) {
	if l != nil {
		l(fmt.Sprintf(format, args...))
	}
}
