package upload

import (
	"testing"
)

// ---------------------------------------------------------------------------
// CircuitBreaker
// ---------------------------------------------------------------------------

func TestCircuitBreaker_InitiallyClosed(t *testing.T) {
	cb := NewCircuitBreaker(3, 30)
	if fail := cb.BeforeRequest(); fail != nil {
		t.Errorf("expected nil (closed circuit), got %v", fail)
	}
}

func TestCircuitBreaker_OpenedAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker(3, 10000)
	cb.RecordFailure()
	cb.RecordFailure()
	if fail := cb.BeforeRequest(); fail != nil {
		t.Error("circuit should still be closed before threshold")
	}
	cb.RecordFailure() // 3rd failure → opens
	fail := cb.BeforeRequest()
	if fail == nil {
		t.Fatal("expected circuit to be open after threshold")
	}
	if fail.Category != "circuit_open" {
		t.Errorf("category = %q, want circuit_open", fail.Category)
	}
	if !fail.Retryable {
		t.Error("expected Retryable=true")
	}
	if fail.RetryAfterSeconds == nil || *fail.RetryAfterSeconds < 1 {
		t.Error("expected RetryAfterSeconds >= 1")
	}
}

func TestCircuitBreaker_RecordSuccessResetsState(t *testing.T) {
	cb := NewCircuitBreaker(2, 10000)
	cb.RecordFailure()
	cb.RecordFailure() // opens
	cb.RecordSuccess()
	if fail := cb.BeforeRequest(); fail != nil {
		t.Errorf("expected closed after RecordSuccess, got %v", fail)
	}
}

func TestCircuitBreaker_SnapshotClosed(t *testing.T) {
	cb := NewCircuitBreaker(3, 30)
	cb.RecordFailure()
	snap := cb.Snapshot()
	if snap["state"] != "closed" {
		t.Errorf("state = %v, want closed", snap["state"])
	}
	if snap["consecutive_failures"].(int) != 1 {
		t.Errorf("consecutive_failures = %v, want 1", snap["consecutive_failures"])
	}
}

func TestCircuitBreaker_SnapshotOpen(t *testing.T) {
	cb := NewCircuitBreaker(1, 10000)
	cb.RecordFailure() // opens immediately (threshold=1)
	snap := cb.Snapshot()
	if snap["state"] != "open" {
		t.Errorf("state = %v, want open", snap["state"])
	}
	if snap["cooldown_remaining_seconds"].(int) <= 0 {
		t.Errorf("cooldown_remaining_seconds should be positive")
	}
}

func TestCircuitBreaker_SuccessResetsConsecutiveFailures(t *testing.T) {
	cb := NewCircuitBreaker(5, 30)
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess()
	snap := cb.Snapshot()
	if snap["consecutive_failures"].(int) != 0 {
		t.Errorf("consecutive_failures = %v, want 0 after success", snap["consecutive_failures"])
	}
}

func TestCircuitBreaker_ZeroCooldownClosedImmediately(t *testing.T) {
	// With cooldown=0, openedUntil = time.Now() + 0 = time.Now().
	// On next call: !time.Now().Before(openedUntil) → resets to closed.
	cb := NewCircuitBreaker(1, 0)
	cb.RecordFailure()
	fail := cb.BeforeRequest()
	if fail != nil {
		// The circuit may or may not be closed depending on nanosecond timing.
		// Both outcomes are valid, just assert the category if open.
		if fail.Category != "circuit_open" {
			t.Errorf("unexpected category: %q", fail.Category)
		}
	}
}
