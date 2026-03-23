package health

import (
	"math/rand"
	"testing"
	"time"
)

func TestDefaultAggregatorConfig(t *testing.T) {
	cfg := DefaultAggregatorConfig()

	if cfg.CriticalThreshold != 10*time.Second {
		t.Errorf("CriticalThreshold = %v, want 10s", cfg.CriticalThreshold)
	}
	if cfg.NonCriticalThreshold != 60*time.Second {
		t.Errorf("NonCriticalThreshold = %v, want 60s", cfg.NonCriticalThreshold)
	}
}

func TestAggregate_NoConditions_ReturnsPending(t *testing.T) {
	cfg := DefaultAggregatorConfig()
	now := time.Now()

	phase := Aggregate(nil, cfg, now)
	if phase != PhasePending {
		t.Errorf("Aggregate(nil) = %q, want %q", phase, PhasePending)
	}

	phase = Aggregate([]ConditionInput{}, cfg, now)
	if phase != PhasePending {
		t.Errorf("Aggregate([]) = %q, want %q", phase, PhasePending)
	}
}

func TestAggregate_AllHealthy_ReturnsReady(t *testing.T) {
	cfg := DefaultAggregatorConfig()
	now := time.Now()

	conditions := []ConditionInput{
		{Name: "GiteaReady", Healthy: true, Critical: true, Since: now.Add(-5 * time.Minute)},
		{Name: "ArgoReady", Healthy: true, Critical: true, Since: now.Add(-5 * time.Minute)},
		{Name: "MetricsReady", Healthy: true, Critical: false, Since: now.Add(-5 * time.Minute)},
	}

	phase := Aggregate(conditions, cfg, now)
	if phase != PhaseReady {
		t.Errorf("Aggregate(all healthy) = %q, want %q", phase, PhaseReady)
	}
}

func TestAggregate_CriticalUnhealthyWithinThreshold_ReturnsProvisioning(t *testing.T) {
	cfg := DefaultAggregatorConfig()
	now := time.Now()

	conditions := []ConditionInput{
		{Name: "GiteaReady", Healthy: false, Critical: true, Since: now.Add(-5 * time.Second)},
		{Name: "ArgoReady", Healthy: true, Critical: true, Since: now.Add(-5 * time.Minute)},
	}

	phase := Aggregate(conditions, cfg, now)
	if phase != PhaseProvisioning {
		t.Errorf("got %q, want %q (critical unhealthy for 5s < 10s threshold)", phase, PhaseProvisioning)
	}
}

func TestAggregate_CriticalUnhealthyBeyondThreshold_ReturnsDegraded(t *testing.T) {
	cfg := DefaultAggregatorConfig()
	now := time.Now()

	conditions := []ConditionInput{
		{Name: "GiteaReady", Healthy: false, Critical: true, Since: now.Add(-15 * time.Second)},
		{Name: "ArgoReady", Healthy: true, Critical: true, Since: now.Add(-5 * time.Minute)},
	}

	phase := Aggregate(conditions, cfg, now)
	if phase != PhaseDegraded {
		t.Errorf("got %q, want %q (critical unhealthy for 15s > 10s threshold)", phase, PhaseDegraded)
	}
}

func TestAggregate_NonCriticalUnhealthyWithinThreshold_ReturnsProvisioning(t *testing.T) {
	cfg := DefaultAggregatorConfig()
	now := time.Now()

	conditions := []ConditionInput{
		{Name: "GiteaReady", Healthy: true, Critical: true, Since: now.Add(-5 * time.Minute)},
		{Name: "MetricsReady", Healthy: false, Critical: false, Since: now.Add(-30 * time.Second)},
	}

	phase := Aggregate(conditions, cfg, now)
	if phase != PhaseProvisioning {
		t.Errorf("got %q, want %q (non-critical unhealthy for 30s < 60s threshold)", phase, PhaseProvisioning)
	}
}

func TestAggregate_NonCriticalUnhealthyBeyondThreshold_ReturnsDegraded(t *testing.T) {
	cfg := DefaultAggregatorConfig()
	now := time.Now()

	conditions := []ConditionInput{
		{Name: "GiteaReady", Healthy: true, Critical: true, Since: now.Add(-5 * time.Minute)},
		{Name: "MetricsReady", Healthy: false, Critical: false, Since: now.Add(-90 * time.Second)},
	}

	phase := Aggregate(conditions, cfg, now)
	if phase != PhaseDegraded {
		t.Errorf("got %q, want %q (non-critical unhealthy for 90s > 60s threshold)", phase, PhaseDegraded)
	}
}

func TestAggregate_CriticalAtExactThreshold_ReturnsDegraded(t *testing.T) {
	cfg := DefaultAggregatorConfig()
	now := time.Now()

	// Exactly at threshold boundary: unhealthy for exactly 10s.
	conditions := []ConditionInput{
		{Name: "GiteaReady", Healthy: false, Critical: true, Since: now.Add(-10 * time.Second)},
	}

	phase := Aggregate(conditions, cfg, now)
	if phase != PhaseDegraded {
		t.Errorf("got %q, want %q (at exact threshold boundary)", phase, PhaseDegraded)
	}
}

func TestAggregate_MixedCriticalAndNonCritical(t *testing.T) {
	cfg := DefaultAggregatorConfig()
	now := time.Now()

	// Critical within threshold + non-critical beyond threshold = Degraded
	// (the worst condition wins)
	conditions := []ConditionInput{
		{Name: "GiteaReady", Healthy: false, Critical: true, Since: now.Add(-5 * time.Second)},
		{Name: "MetricsReady", Healthy: false, Critical: false, Since: now.Add(-90 * time.Second)},
	}

	phase := Aggregate(conditions, cfg, now)
	if phase != PhaseDegraded {
		t.Errorf("got %q, want %q (non-critical exceeded threshold)", phase, PhaseDegraded)
	}
}

// Property test: health never oscillates faster than the shortest threshold.
func TestAggregate_PropertyNoOscillation(t *testing.T) {
	cfg := DefaultAggregatorConfig()
	rng := rand.New(rand.NewSource(42))

	// Simulate 200 random condition sequences and verify that transitioning
	// from Ready to Degraded always requires at least CriticalThreshold duration.
	for i := 0; i < 200; i++ {
		numConditions := rng.Intn(5) + 1
		conditions := make([]ConditionInput, numConditions)
		baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

		for j := 0; j < numConditions; j++ {
			conditions[j] = ConditionInput{
				Name:     "cond",
				Healthy:  true,
				Critical: rng.Intn(2) == 0,
				Since:    baseTime,
			}
		}

		// At t=0 all healthy -> Ready
		phase := Aggregate(conditions, cfg, baseTime)
		if phase != PhaseReady {
			t.Fatalf("iteration %d: all healthy should be Ready, got %q", i, phase)
		}

		// Make one condition unhealthy at t=0
		unhealthyIdx := rng.Intn(numConditions)
		conditions[unhealthyIdx].Healthy = false
		conditions[unhealthyIdx].Since = baseTime

		threshold := cfg.NonCriticalThreshold
		if conditions[unhealthyIdx].Critical {
			threshold = cfg.CriticalThreshold
		}

		// At t=threshold-1ms: should NOT be Degraded yet
		justBefore := baseTime.Add(threshold - time.Millisecond)
		phase = Aggregate(conditions, cfg, justBefore)
		if phase == PhaseDegraded {
			t.Fatalf("iteration %d: should not be Degraded before threshold (%v), got Degraded at %v since %v",
				i, threshold, justBefore.Sub(baseTime), conditions[unhealthyIdx].Since.Sub(baseTime))
		}

		// At t=threshold: should be Degraded
		atThreshold := baseTime.Add(threshold)
		phase = Aggregate(conditions, cfg, atThreshold)
		if phase != PhaseDegraded {
			t.Fatalf("iteration %d: should be Degraded at threshold (%v), got %q",
				i, threshold, phase)
		}
	}
}
