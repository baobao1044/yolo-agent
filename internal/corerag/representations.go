package corerag

import "strings"

// Materialize renders a symbol at the given SACRS compression level, per
// CORE doc Table 3 and the four design principles (§4.2.1):
//
//   - Principle 1: preserve type information (DTOs keep field types)
//   - Principle 2: preserve call interfaces (Services keep signatures)
//   - Principle 3: preserve framework semantics (Controllers keep annotations)
//   - Principle 4: strip executable implementation details (Utilities)
//
// Level k=0 is raw source (full body); k=1 keeps signature + docs + (for
// Controllers) decorators; k=2 keeps only the interface signature; k=3 keeps
// the symbol name only (a stub).
func Materialize(s Symbol, k Level) string {
	switch k {
	case LevelRaw:
		return s.raw()
	case LevelSigDocs:
		return s.sigDocs()
	case LevelInterface:
		return s.interfaceSig()
	case LevelStub:
		return s.name()
	}
	// Defensive: unknown levels fall back to stub (never over-compress).
	if k > LevelStub {
		return s.name()
	}
	return s.raw()
}

// raw returns the full source representation: signature + body + doc.
func (s Symbol) raw() string {
	var b strings.Builder
	if s.Doc != "" {
		b.WriteString(s.Doc)
		b.WriteString("\n")
	}
	b.WriteString(s.Signature)
	if s.Body != "" {
		b.WriteString(" ")
		b.WriteString(s.Body)
	}
	return b.String()
}

// sigDocs keeps the signature and API docs but drops the implementation body.
// For Controllers, decorators/annotations (carried in the Doc/signature text)
// are preserved by keeping the Doc, honoring Principle 3.
func (s Symbol) sigDocs() string {
	var b strings.Builder
	if s.Doc != "" {
		b.WriteString(s.Doc)
		b.WriteString("\n")
	}
	b.WriteString(s.Signature)
	return b.String()
}

// interfaceSig keeps only the interface signature: the Signature line with no
// doc and no body. For DTOs this retains the type/field declaration; for
// services the function signature (parameters + return type).
func (s Symbol) interfaceSig() string {
	return s.Signature
}

// name returns the stub: just the symbol name.
func (s Symbol) name() string {
	return s.Name
}
