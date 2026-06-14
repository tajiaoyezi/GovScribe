package gateway

import (
	"context"
	"sync"
	"time"
)

type NERCircuitBreakerConfig struct {
	FailureThreshold int
	OpenInterval     time.Duration
}

type NERCircuitBreaker struct {
	client           NERClient
	failureThreshold int
	openInterval     time.Duration
	now              func() time.Time
	mu               sync.Mutex
	failures         int
	openUntil        time.Time
}

func NewNERCircuitBreaker(client NERClient, cfg NERCircuitBreakerConfig) *NERCircuitBreaker {
	threshold := cfg.FailureThreshold
	if threshold <= 0 {
		threshold = 3
	}
	openInterval := cfg.OpenInterval
	if openInterval <= 0 {
		openInterval = 30 * time.Second
	}
	return &NERCircuitBreaker{
		client:           client,
		failureThreshold: threshold,
		openInterval:     openInterval,
		now:              time.Now,
	}
}

func (b *NERCircuitBreaker) Recognize(ctx context.Context, text string) ([]Hit, error) {
	if b.client == nil {
		return nil, ErrNERUnavailable
	}
	b.mu.Lock()
	now := b.now()
	if !b.openUntil.IsZero() && now.Before(b.openUntil) {
		b.mu.Unlock()
		return nil, ErrNERCircuitOpen
	}
	b.mu.Unlock()

	hits, err := b.client.Recognize(ctx, text)
	b.mu.Lock()
	defer b.mu.Unlock()
	now = b.now()
	if err != nil {
		b.failures++
		if b.failures >= b.failureThreshold {
			b.openUntil = now.Add(b.openInterval)
		}
		return nil, err
	}
	b.failures = 0
	b.openUntil = time.Time{}
	return hits, nil
}
