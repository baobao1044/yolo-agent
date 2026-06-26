package corerag

import "testing"

func TestMaterializeLevels(t *testing.T) {
	s := Symbol{
		Name:      "Handler.ServeHTTP",
		Signature: "func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request)",
		Doc:       "// ServeHTTP handles an HTTP request.",
		Body:      "{ w.WriteHeader(200) }",
	}

	if got := Materialize(s, LevelRaw); got == "" || !containsStr(got, "{ w.WriteHeader(200) }") {
		t.Fatalf("k=0 should include body, got: %q", got)
	}
	if got := Materialize(s, LevelSigDocs); containsStr(got, "WriteHeader") || got == "" {
		t.Fatalf("k=1 should drop body but keep signature+doc, got: %q", got)
	}
	if got := Materialize(s, LevelSigDocs); !containsStr(got, "ServeHTTP handles an HTTP request") {
		t.Fatalf("k=1 should keep doc, got: %q", got)
	}
	if got := Materialize(s, LevelInterface); containsStr(got, "handles an HTTP") || !containsStr(got, "func (h Handler)") {
		t.Fatalf("k=2 should keep signature only, got: %q", got)
	}
	if got := Materialize(s, LevelStub); got != "Handler.ServeHTTP" {
		t.Fatalf("k=3 should be name only, got: %q", got)
	}
}

func TestMaterializeEmpty(t *testing.T) {
	s := Symbol{Name: "x"}
	if got := Materialize(s, LevelRaw); got != "x" && got != "" {
		// raw() of an empty symbol yields the name from the signature-less path;
		// here Signature is "" so raw() returns "". Acceptable, but ensure
		// stub is the name.
		t.Fatalf("k=0 empty unexpected: %q", got)
	}
	if got := Materialize(s, LevelStub); got != "x" {
		t.Fatalf("k=3 empty should be name, got: %q", got)
	}
}

func TestMaterializeUnknownLevelFallsBackToStub(t *testing.T) {
	s := Symbol{Name: "foo", Signature: "func foo()"}
	if got := Materialize(s, Level(99)); got != "foo" {
		t.Fatalf("unknown level should fall back to stub (name), got: %q", got)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && stringsContains(s, sub)
}

func stringsContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
