package llm

import (
	"context"
	"errors"
	"testing"
)

func TestChatRequestPreservesMissingContentSecurityLevel(t *testing.T) {
	req := ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "起草一份会议通知"}},
	}

	if req.HasContentSecurityLevel() {
		t.Fatal("missing content security level must stay distinguishable")
	}
	if req.ContentSecurityLevel != ContentSecurityLevelUnknown {
		t.Fatalf("missing level = %q, want unknown", req.ContentSecurityLevel)
	}
}

func TestChatRequestPreservesExplicitContentSecurityLevel(t *testing.T) {
	req := ChatRequest{
		Messages:             []Message{{Role: RoleUser, Content: "起草一份会议通知"}},
		ContentSecurityLevel: ContentSecurityLevelSensitive,
	}

	if !req.HasContentSecurityLevel() {
		t.Fatal("explicit content security level must be visible to c02")
	}
	if req.ContentSecurityLevel != ContentSecurityLevelSensitive {
		t.Fatalf("level = %q, want sensitive", req.ContentSecurityLevel)
	}
}

func TestProviderErrorClassifiesNoAvailablePrivateConfig(t *testing.T) {
	err := &ProviderError{Reason: ErrorReasonNoAvailablePrivateConfig}

	if !errors.Is(err, ErrNoAvailablePrivateConfig) {
		t.Fatalf("expected errors.Is to match ErrNoAvailablePrivateConfig, got %v", err)
	}
	if err.Reason != ErrorReasonNoAvailablePrivateConfig {
		t.Fatalf("reason = %q, want no available private config", err.Reason)
	}
}

func TestClientInterfaceCoversCompletionStreamAndNetworkState(t *testing.T) {
	var client Client = fakeClient{network: NetworkPrivate}

	resp, err := client.Complete(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "写一个通知"}},
		Route:    Route{ConfigID: "private-1"},
	})
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}
	if resp.FinishReason != FinishReasonStop {
		t.Fatalf("finish reason = %q, want stop", resp.FinishReason)
	}

	events, err := client.Stream(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "流式写一个通知"}},
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}
	first := <-events
	if first.Type != StreamEventTypeDelta || first.Delta != "ok" {
		t.Fatalf("first event = %#v, want delta ok", first)
	}
	done := <-events
	if done.Type != StreamEventTypeDone || done.FinishReason != FinishReasonStop {
		t.Fatalf("done event = %#v, want stop done", done)
	}

	network, err := client.CurrentNetwork(context.Background())
	if err != nil {
		t.Fatalf("current network failed: %v", err)
	}
	if network != NetworkPrivate {
		t.Fatalf("network = %q, want private", network)
	}
}

type fakeClient struct {
	network Network
}

func (f fakeClient) Complete(context.Context, ChatRequest) (ChatResponse, error) {
	return ChatResponse{Text: "ok", FinishReason: FinishReasonStop}, nil
}

func (f fakeClient) Stream(context.Context, ChatRequest) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent, 2)
	ch <- StreamEvent{Type: StreamEventTypeDelta, Delta: "ok"}
	ch <- StreamEvent{Type: StreamEventTypeDone, FinishReason: FinishReasonStop}
	close(ch)
	return ch, nil
}

func (f fakeClient) CurrentNetwork(context.Context) (Network, error) {
	return f.network, nil
}
