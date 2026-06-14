package gateway

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNERCircuitBreakerOpensAfterConsecutiveFailuresAndHalfOpenCloses(t *testing.T) {
	now := time.Unix(1700000000, 0)
	ner := &scriptedNER{
		results: []nerResult{
			{err: ErrNERUnavailable},
			{err: ErrNERUnavailable},
			{hits: []Hit{{Start: 0, End: len("张三"), Text: "张三", Type: EntityTypePerson, Source: SourceNER}}},
		},
	}
	breaker := NewNERCircuitBreaker(ner, NERCircuitBreakerConfig{
		FailureThreshold: 2,
		OpenInterval:     time.Minute,
	})
	breaker.now = func() time.Time { return now }

	for i := 0; i < 2; i++ {
		if _, err := breaker.Recognize(context.Background(), "张三"); !errors.Is(err, ErrNERUnavailable) {
			t.Fatalf("failure %d error = %v, want ErrNERUnavailable", i+1, err)
		}
	}
	if ner.calls != 2 {
		t.Fatalf("calls = %d, want 2 before circuit opens", ner.calls)
	}

	if _, err := breaker.Recognize(context.Background(), "张三"); !errors.Is(err, ErrNERCircuitOpen) {
		t.Fatalf("open circuit error = %v, want ErrNERCircuitOpen", err)
	}
	if ner.calls != 2 {
		t.Fatalf("open circuit called upstream, calls = %d", ner.calls)
	}

	now = now.Add(time.Minute)
	hits, err := breaker.Recognize(context.Background(), "张三")
	if err != nil {
		t.Fatalf("half-open recognize: %v", err)
	}
	assertHasHit(t, hits, EntityTypePerson, SourceNER, "张三")

	if _, err := breaker.Recognize(context.Background(), "张三"); err != nil {
		t.Fatalf("closed circuit should call upstream without open error: %v", err)
	}
	if ner.calls != 4 {
		t.Fatalf("calls = %d, want half-open success to close circuit", ner.calls)
	}
}

type scriptedNER struct {
	results []nerResult
	calls   int
}

type nerResult struct {
	hits []Hit
	err  error
}

func (n *scriptedNER) Recognize(context.Context, string) ([]Hit, error) {
	n.calls++
	if len(n.results) == 0 {
		return nil, nil
	}
	result := n.results[0]
	n.results = n.results[1:]
	return result.hits, result.err
}
