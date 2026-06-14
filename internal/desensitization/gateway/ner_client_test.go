package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPNERClientSendsOnlyTextAndMapsSpans(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"entities": []map[string]any{
				{"start": 0, "end": len("张三"), "type": "person"},
				{"start": len("张三在"), "end": len("张三在海淀区"), "type": "location"},
			},
		})
	}))
	defer server.Close()

	client := NewHTTPNERClient(server.URL, server.Client())
	hits, err := client.Recognize(context.Background(), "张三在海淀区")
	if err != nil {
		t.Fatalf("recognize: %v", err)
	}
	if _, ok := got["mappings"]; ok {
		t.Fatalf("request leaked mappings field: %#v", got)
	}
	if got["text"] != "张三在海淀区" || len(got) != 1 {
		t.Fatalf("request body = %#v, want only text", got)
	}
	assertHasHit(t, hits, EntityTypePerson, SourceNER, "张三")
	assertHasHit(t, hits, EntityTypeNamedEntity, SourceNER, "海淀区")
}

func TestHTTPNERClientTreatsUnavailableAndBadResponsesAsUnavailable(t *testing.T) {
	t.Run("5xx", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "down", http.StatusBadGateway)
		}))
		defer server.Close()

		_, err := NewHTTPNERClient(server.URL, server.Client()).Recognize(context.Background(), "张三")
		if !errors.Is(err, ErrNERUnavailable) {
			t.Fatalf("error = %v, want ErrNERUnavailable", err)
		}
	})

	t.Run("bad span in 200 response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"entities": []map[string]any{{"start": 2, "end": 200, "type": "person"}},
			})
		}))
		defer server.Close()

		_, err := NewHTTPNERClient(server.URL, server.Client()).Recognize(context.Background(), "张三")
		if !errors.Is(err, ErrNERUnavailable) {
			t.Fatalf("error = %v, want ErrNERUnavailable", err)
		}
	})

	t.Run("missing entities field", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{}`))
		}))
		defer server.Close()

		_, err := NewHTTPNERClient(server.URL, server.Client()).Recognize(context.Background(), "张三")
		if !errors.Is(err, ErrNERUnavailable) {
			t.Fatalf("error = %v, want ErrNERUnavailable", err)
		}
	})

	t.Run("null entities field", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"entities":null}`))
		}))
		defer server.Close()

		_, err := NewHTTPNERClient(server.URL, server.Client()).Recognize(context.Background(), "张三")
		if !errors.Is(err, ErrNERUnavailable) {
			t.Fatalf("error = %v, want ErrNERUnavailable", err)
		}
	})

	t.Run("missing entity type", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"entities":[{"start":0,"end":6}]}`))
		}))
		defer server.Close()

		_, err := NewHTTPNERClient(server.URL, server.Client()).Recognize(context.Background(), "张三")
		if !errors.Is(err, ErrNERUnavailable) {
			t.Fatalf("error = %v, want ErrNERUnavailable", err)
		}
	})

	t.Run("span splits utf8 rune", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"entities": []map[string]any{{"start": 1, "end": len("张三"), "type": "person"}},
			})
		}))
		defer server.Close()

		_, err := NewHTTPNERClient(server.URL, server.Client()).Recognize(context.Background(), "张三")
		if !errors.Is(err, ErrNERUnavailable) {
			t.Fatalf("error = %v, want ErrNERUnavailable", err)
		}
	})

	t.Run("transport timeout or cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := NewHTTPNERClient("http://127.0.0.1:1", http.DefaultClient).Recognize(ctx, "张三")
		if !errors.Is(err, ErrNERUnavailable) {
			t.Fatalf("error = %v, want ErrNERUnavailable", err)
		}
	})
}

func TestHTTPNERClientUsesSafeDefaultTimeout(t *testing.T) {
	client := NewHTTPNERClient("http://127.0.0.1:1", nil)
	if client.httpClient == nil {
		t.Fatal("http client is nil")
	}
	if client.httpClient.Timeout <= 0 {
		t.Fatalf("default timeout = %s, want positive timeout", client.httpClient.Timeout)
	}
}
