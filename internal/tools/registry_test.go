package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRespondTool(t *testing.T) {
	tool := NewRespondTool()

	if tool.Name() != "respond" {
		t.Fatalf("expected tool name 'respond', got %s", tool.Name())
	}

	args, _ := json.Marshal(map[string]string{"message": "hello"})
	res, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	answer, ok := res.(RespondResult)
	if !ok {
		t.Fatalf("expected RespondResult, got %T", res)
	}

	if answer.Message != "hello" || !answer.Done {
		t.Fatalf("unexpected result: %+v", answer)
	}
}

func TestRegistry(t *testing.T) {
	reg := NewRegistry()
	reg.MustRegister(NewRespondTool())

	if reg.Size() != 1 {
		t.Fatalf("expected 1 tool, got %d", reg.Size())
	}

	tool, ok := reg.Get("respond")
	if !ok {
		t.Fatalf("expected respond tool to exist")
	}

	if tool.Name() != "respond" {
		t.Fatalf("expected respond, got %s", tool.Name())
	}
}
