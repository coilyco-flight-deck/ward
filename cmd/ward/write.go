package main

import (
	"fmt"
	"io"
)

// writef and writeln are best-effort emitters for CLI/status output.
func writef(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

func writeln(w io.Writer, args ...any) {
	_, _ = fmt.Fprintln(w, args...)
}
