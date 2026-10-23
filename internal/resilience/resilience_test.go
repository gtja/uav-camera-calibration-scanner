package resilience

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCircuitBreakerRetriesUntilSuccess(t *testing.T) {
	breaker := NewCircuitBreaker("test", 5, time.Minute)
	var calls int
	err := breaker.Execute(context.Background(), RetryConfig{
		Attempts:  3,
		BaseDelay: time.Nanosecond,
	}, func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("temporary failure")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestCircuitBreakerOpensAfterThreshold(t *testing.T) {
	breaker := NewCircuitBreaker("test", 2, time.Minute)
	fn := func(context.Context) error {
		return errors.New("downstream unavailable")
	}
	for i := 0; i < 2; i++ {
		if err := breaker.Execute(context.Background(), RetryConfig{Attempts: 1}, fn); err == nil {
			t.Fatalf("Execute() error = nil, want failure")
		}
	}
	err := breaker.Execute(context.Background(), RetryConfig{Attempts: 1}, fn)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("Execute() error = %v, want ErrCircuitOpen", err)
	}
}
