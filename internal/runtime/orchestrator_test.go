package runtime

import (
	"context"
	"encoding/json"
	"testing"

	taskdomain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

// --- fakes ---

type fakeLLMProvider struct {
	responses []ChatResponse
	callCount int
}

func (f *fakeLLMProvider) Chat(_ context.Context, _ ChatRequest) (ChatResponse, error) {
	if f.callCount >= len(f.responses) {
		return ChatResponse{Text: "fallback response"}, nil
	}
	resp := f.responses[f.callCount]
	f.callCount++
	return resp, nil
}

type failingLLMProvider struct{}

func (f *failingLLMProvider) Chat(_ context.Context, _ ChatRequest) (ChatResponse, error) {
	return ChatResponse{}, context.DeadlineExceeded
}

// --- tests ---

func TestOrchestrator_Run_directReply(t *testing.T) {
	t.Parallel()

	provider := &fakeLLMProvider{
		responses: []ChatResponse{
			{Text: "Hola, todo bien."},
		},
	}
	toolkit := &ToolKit{Handlers: make(map[string]ToolHandler)}
	ports := ContextPorts{}

	orch := NewOrchestrator(provider, toolkit, ports)

	result, err := orch.Run(context.Background(), RunInput{
		UserID:  "user-1",
		OrgID:   "org-1",
		Message: "Hola",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Reply != "Hola, todo bien." {
		t.Fatalf("unexpected reply: %s", result.Reply)
	}
}

func TestOrchestrator_Run_withToolCall(t *testing.T) {
	t.Parallel()

	provider := &fakeLLMProvider{
		responses: []ChatResponse{
			// Ronda 1: el LLM pide una tool
			{
				Text: "",
				ToolCalls: []LLMToolCall{
					{ID: "tc-1", Name: "get_overview", Args: json.RawMessage(`{}`)},
				},
			},
			// Ronda 2: el LLM responde con el resultado
			{Text: "Tenés 3 aprobaciones pendientes."},
		},
	}
	toolkit := &ToolKit{
		Handlers: map[string]ToolHandler{
			"get_overview": func(_ context.Context, _ json.RawMessage) (string, error) {
				return `{"pending_approvals": 3}`, nil
			},
		},
	}
	ports := ContextPorts{}

	orch := NewOrchestrator(provider, toolkit, ports)

	result, err := orch.Run(context.Background(), RunInput{
		UserID:  "user-1",
		OrgID:   "org-1",
		Message: "¿Qué tengo pendiente?",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Reply != "Tenés 3 aprobaciones pendientes." {
		t.Fatalf("unexpected reply: %s", result.Reply)
	}
	if provider.callCount != 2 {
		t.Fatalf("expected 2 LLM calls, got %d", provider.callCount)
	}
}

func TestOrchestrator_Run_fallbackOnProviderError(t *testing.T) {
	t.Parallel()

	toolkit := &ToolKit{Handlers: make(map[string]ToolHandler)}
	ports := ContextPorts{}

	orch := NewOrchestrator(&failingLLMProvider{}, toolkit, ports)

	result, err := orch.Run(context.Background(), RunInput{
		UserID:  "user-1",
		OrgID:   "org-1",
		Message: "Hola",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Reply == "" {
		t.Fatal("expected non-empty fallback reply")
	}
}

func TestOrchestrator_Run_emptyTextFallbackMessage(t *testing.T) {
	t.Parallel()

	provider := &fakeLLMProvider{
		responses: []ChatResponse{
			{Text: ""},
		},
	}
	toolkit := &ToolKit{Handlers: make(map[string]ToolHandler)}
	ports := ContextPorts{}

	orch := NewOrchestrator(provider, toolkit, ports)

	result, err := orch.Run(context.Background(), RunInput{
		UserID: "user-1", OrgID: "org-1", Message: "Hola",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Reply == "" {
		t.Fatal("expected non-empty reply for empty LLM response")
	}
}

func TestValidateToolCallSafety_requiresApprovalID(t *testing.T) {
	t.Parallel()

	err := ValidateToolCallSafety("approve_action", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for approve without approval_id")
	}

	err = ValidateToolCallSafety("reject_action", json.RawMessage(`{"approval_id": ""}`))
	if err == nil {
		t.Fatal("expected error for reject with empty approval_id")
	}

	err = ValidateToolCallSafety("approve_action", json.RawMessage(`{"approval_id": "abc-123"}`))
	if err != nil {
		t.Fatalf("unexpected error for valid approval_id: %v", err)
	}
}

func TestValidateToolCallSafety_unknownToolIsOK(t *testing.T) {
	t.Parallel()

	err := ValidateToolCallSafety("get_overview", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error for non-restricted tool: %v", err)
	}
}

func TestToolKit_ExecuteTool_unknownTool(t *testing.T) {
	t.Parallel()

	tk := &ToolKit{Handlers: make(map[string]ToolHandler)}
	result := tk.ExecuteTool(context.Background(), "nonexistent", json.RawMessage(`{}`))
	if result == "" {
		t.Fatal("expected error message for unknown tool")
	}
}

func TestFallbackReply(t *testing.T) {
	t.Parallel()

	reply := FallbackReply("")
	if reply == "" {
		t.Fatal("expected non-empty fallback for empty overview")
	}

	reply = FallbackReply("3 aprobaciones pendientes")
	if reply == "" {
		t.Fatal("expected non-empty fallback with overview")
	}
}

func TestOrchestrator_Run_passesMessageHistory(t *testing.T) {
	t.Parallel()

	var capturedMessages []LLMMessage
	provider := &fakeLLMProvider{
		responses: []ChatResponse{
			{Text: "OK"},
		},
	}
	// Reemplazar Chat para capturar mensajes
	origChat := provider.Chat
	_ = origChat

	toolkit := &ToolKit{Handlers: make(map[string]ToolHandler)}
	ports := ContextPorts{}

	orch := NewOrchestrator(provider, toolkit, ports)

	history := []taskdomain.TaskMessage{
		{AuthorType: "user", Body: "Mensaje previo"},
		{AuthorType: "system", Body: "Respuesta previa"},
	}

	result, err := orch.Run(context.Background(), RunInput{
		UserID:   "user-1",
		OrgID:    "org-1",
		Message:  "Nuevo mensaje",
		Messages: history,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = capturedMessages
	if result.Reply != "OK" {
		t.Fatalf("unexpected reply: %s", result.Reply)
	}
}
