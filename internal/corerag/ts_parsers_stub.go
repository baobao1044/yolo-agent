//go:build !cgo

package corerag

import "log/slog"

// newTreeSitterParsers returns nil when the binary is built without cgo,
// because tree-sitter grammars require cgo. The engine then only registers
// the Go (stdlib) parser, so Code-RAG still works for Go repos (the dogfood
// path) even without a C toolchain.
func newTreeSitterParsers(_ *slog.Logger) map[string]Parser {
	return nil
}
