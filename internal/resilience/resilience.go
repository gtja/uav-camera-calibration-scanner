package resilience

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

var ErrCircuitOpen = errors.New("circuit breaker is open")

type RetryConfig struct {
	Attempts  int
	BaseDelay time.Duration
}

type CircuitBreaker struct {
	name             string
	failureThreshold int
	openFor          time.Duration

	mu                  sync.Mutex
	consecutiveFailures int
	openedUntil         time.Time
}

func NewCircuitBreaker(name string, failureThreshold int, openFor time.Duration) *CircuitBreaker {
	if failureThreshold <= 0 {
		failureThreshold = 3
	}
	if openFor <= 0 {
		openFor = 5 * time.Minute
	}
	return &CircuitBreaker{
		name:             name,
		failureThreshold: failureThreshold,
		openFor:          openFor,
	}
}

func (b *CircuitBreaker) Execute(ctx context.Context, retry RetryConfig, fn func(context.Context) error) error {
	attempts := retry.Attempts
	if attempts <= 0 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := b.beforeCall(); err != nil {
			slog.Warn("circuit breaker rejected external call", "dependency", b.name, "error", err)
			return err
		}
		err := fn(ctx)
		if err == nil {
			b.afterSuccess()
			return nil
		}
		lastErr = err
		b.afterFailure()
		slog.Warn("external call failed", "dependency", b.name, "attempt", attempt, "attempts", attempts, "error", err)
		if attempt == attempts {
			break
		}
		delay := retry.BaseDelay
		if delay <= 0 {
			delay = 500 * time.Millisecond
		}
		delay = delay * time.Duration(1<<(attempt-1))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	if lastErr == nil {
		return nil
	}
	return lastErr
}

func (b *CircuitBreaker) beforeCall() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.openedUntil.IsZero() {
		return nil
	}
	if time.Now().Before(b.openedUntil) {
		return fmt.Errorf("%w: %s until %s", ErrCircuitOpen, b.name, b.openedUntil.Format(time.RFC3339))
	}
	b.openedUntil = time.Time{}
	b.consecutiveFailures = 0
	return nil
}

func (b *CircuitBreaker) afterSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.consecutiveFailures = 0
	b.openedUntil = time.Time{}
}

func (b *CircuitBreaker) afterFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.consecutiveFailures++
	if b.consecutiveFailures >= b.failureThreshold {
		b.openedUntil = time.Now().Add(b.openFor)
		slog.Warn(
			"circuit breaker opened",
			"dependency", b.name,
			"consecutiveFailures", b.consecutiveFailures,
			"openUntil", b.openedUntil.Format(time.RFC3339),
		)
	}
}
