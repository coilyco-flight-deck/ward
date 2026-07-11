// Package scripts holds the release pipeline's shell helpers plus the Go tests
// that exercise them, so `make test` covers release-notes rendering the same way
// it covers the ward binary. The tested artifacts are the scripts beside this
// package, driven here with synthetic fixtures instead of live git state.
package scripts
