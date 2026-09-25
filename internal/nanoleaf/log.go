package nanoleaf

import "log"

// debugf writes one debug line if a logger was provided.
//
// Every type in this package that does network work carries a `Log *log.Logger`
// field. When the CLI is run without --debug that field stays nil, and this
// helper turns each debug call into a no-op. Centralising the nil check here
// keeps the call sites short: debugf(d.Log, "...", args...).
//
// `format string, args ...any` is a variadic parameter list, like printf in C.
// `any` is Go's alias for the empty interface: a value of any type.
func debugf(l *log.Logger, format string, args ...any) {
	if l == nil {
		return
	}
	l.Printf(format, args...)
}
