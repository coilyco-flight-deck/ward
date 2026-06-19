//go:build !unix

package main

import "os"

// flock fallback for non-unix builds: the entrypoint only runs as container
// PID 1 (Linux), so concurrent-warming serialisation degrades to a no-op here.

func flockExclusive(_ *os.File) error { return nil }

func flockUnlock(_ *os.File) error { return nil }
