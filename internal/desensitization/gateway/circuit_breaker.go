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
	state            nerCircuitState
	openUntil        time.Time
	probeInFlight    bool
}

type nerCircuitState int

const (
	nerCircuitClosed nerCircuitState = iota
	nerCircuitOpen
	nerCircuitHalfOpen
)

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
	switch b.state {
	case nerCircuitOpen:
		if now.Before(b.openUntil) {
			b.mu.Unlock()
			return nil, ErrNERCircuitOpen
		}
		b.state = nerCircuitHalfOpen
	case nerCircuitHalfOpen:
		if b.probeInFlight {
			b.mu.Unlock()
			return nil, ErrNERCircuitOpen
		}
	}
	halfOpenProbe := b.state == nerCircuitHalfOpen
	if halfOpenProbe {
		b.probeInFlight = true
	}
	b.mu.Unlock()

	hits, err := b.client.Recognize(ctx, text)
	b.mu.Lock()
	defer b.mu.Unlock()
	if halfOpenProbe {
		b.probeInFlight = false
	}
	now = b.now()
	if err != nil {
		if halfOpenProbe {
			b.open(now)
			return nil, err
		}
		if b.state == nerCircuitClosed {
			b.failures++
			if b.failures >= b.failureThreshold {
				b.open(now)
			}
		}
		return nil, err
	}
	b.failures = 0
	b.state = nerCircuitClosed
	b.openUntil = time.Time{}
	return hits, nil
}

func (b *NERCircuitBreaker) open(now time.Time) {
	b.state = nerCircuitOpen
	b.failures = b.failureThreshold
	b.openUntil = now.Add(b.openInterval)
}
