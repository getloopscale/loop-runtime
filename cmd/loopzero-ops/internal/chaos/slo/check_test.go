// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package slo

import (
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/exitcode"
)

func ip(n int) *int { return &n }

func TestCompare_HeldAndRecovered_Pass(t *testing.T) {
	pre := Sample{P95ms: ip(100), SuccessPct: ip(100), LoopZeroAPIRestarts: ip(0)}
	during := Sample{P95ms: ip(120), SuccessPct: ip(95), LoopZeroAPIRestarts: ip(0)}
	post := Sample{P95ms: ip(105), SuccessPct: ip(100), LoopZeroAPIRestarts: ip(1)}
	v := Compare(pre, during, post, DefaultThresholds())
	if v.Result != Pass {
		t.Errorf("want PASS, got %s (reasons=%v)", v.Result, v.Reasons)
	}
	if v.Result.ExitCode() != 0 {
		t.Errorf("PASS exit code want 0, got %d", v.Result.ExitCode())
	}
}

func TestCompare_SuccessRateBreach_Fail(t *testing.T) {
	pre := Sample{P95ms: ip(100), SuccessPct: ip(100), LoopZeroAPIRestarts: ip(0)}
	during := Sample{P95ms: ip(110), SuccessPct: ip(80), LoopZeroAPIRestarts: ip(0)} // 80 < 90
	post := Sample{P95ms: ip(100), SuccessPct: ip(100), LoopZeroAPIRestarts: ip(0)}
	v := Compare(pre, during, post, DefaultThresholds())
	if v.Result != Fail {
		t.Fatalf("want FAIL, got %s", v.Result)
	}
	if v.Result.ExitCode() != exitcode.Failure {
		t.Errorf("FAIL exit want %d, got %d", exitcode.Failure, v.Result.ExitCode())
	}
	if len(v.Reasons) != 1 {
		t.Errorf("want 1 reason, got %v", v.Reasons)
	}
}

func TestCompare_LatencyDegradationBreach_Fail(t *testing.T) {
	pre := Sample{P95ms: ip(100), SuccessPct: ip(100), LoopZeroAPIRestarts: ip(0)}
	during := Sample{P95ms: ip(650), SuccessPct: ip(99), LoopZeroAPIRestarts: ip(0)} // +550 > 500
	post := Sample{P95ms: ip(100), SuccessPct: ip(100), LoopZeroAPIRestarts: ip(0)}
	if Compare(pre, during, post, DefaultThresholds()).Result != Fail {
		t.Error("want FAIL on during p95 degradation > 500ms")
	}
}

func TestCompare_PostDriftBreach_Fail(t *testing.T) {
	pre := Sample{P95ms: ip(100), SuccessPct: ip(100), LoopZeroAPIRestarts: ip(0)}
	during := Sample{P95ms: ip(120), SuccessPct: ip(99), LoopZeroAPIRestarts: ip(0)}
	post := Sample{P95ms: ip(250), SuccessPct: ip(100), LoopZeroAPIRestarts: ip(0)} // +150 > 100
	if Compare(pre, during, post, DefaultThresholds()).Result != Fail {
		t.Error("want FAIL on post drift > 100ms")
	}
}

func TestCompare_RestartDeltaBreach_Fail(t *testing.T) {
	pre := Sample{P95ms: ip(100), SuccessPct: ip(100), LoopZeroAPIRestarts: ip(0)}
	during := Sample{P95ms: ip(110), SuccessPct: ip(99), LoopZeroAPIRestarts: ip(0)}
	post := Sample{P95ms: ip(100), SuccessPct: ip(100), LoopZeroAPIRestarts: ip(3)} // delta 3 > 2
	if Compare(pre, during, post, DefaultThresholds()).Result != Fail {
		t.Error("want FAIL on restart delta > 2")
	}
}

func TestCompare_MultipleBreaches_AllReasons(t *testing.T) {
	pre := Sample{P95ms: ip(100), SuccessPct: ip(100), LoopZeroAPIRestarts: ip(0)}
	during := Sample{P95ms: ip(700), SuccessPct: ip(50), LoopZeroAPIRestarts: ip(0)} // success + p95 breach
	post := Sample{P95ms: ip(300), SuccessPct: ip(100), LoopZeroAPIRestarts: ip(5)}  // drift + restart breach
	v := Compare(pre, during, post, DefaultThresholds())
	if v.Result != Fail || len(v.Reasons) != 4 {
		t.Errorf("want FAIL with 4 reasons, got %s with %d: %v", v.Result, len(v.Reasons), v.Reasons)
	}
}

func TestCompare_MissingMetric_Inconclusive(t *testing.T) {
	pre := Sample{P95ms: nil, SuccessPct: ip(100), LoopZeroAPIRestarts: ip(0)} // missing pre p95
	during := Sample{P95ms: ip(110), SuccessPct: ip(99), LoopZeroAPIRestarts: ip(0)}
	post := Sample{P95ms: ip(100), SuccessPct: ip(100), LoopZeroAPIRestarts: ip(0)}
	v := Compare(pre, during, post, DefaultThresholds())
	if v.Result != Inconclusive {
		t.Errorf("want INCONCLUSIVE on missing metric, got %s", v.Result)
	}
	if v.Result.ExitCode() != 5 {
		t.Errorf("INCONCLUSIVE exit want 5, got %d", v.Result.ExitCode())
	}
}

func TestParseSample(t *testing.T) {
	data := []byte(`{"api_health":{"latency_p95_ms":123,"success_rate_pct":98},"pod_restart_counts":{"loopzero_api":2}}`)
	s, err := ParseSample(data)
	if err != nil {
		t.Fatal(err)
	}
	if s.P95ms == nil || *s.P95ms != 123 || s.SuccessPct == nil || *s.SuccessPct != 98 || s.LoopZeroAPIRestarts == nil || *s.LoopZeroAPIRestarts != 2 {
		t.Errorf("parsed wrong: %+v", s)
	}
}

func TestParseSample_MissingFields_NilNotZero(t *testing.T) {
	s, err := ParseSample([]byte(`{"api_health":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.P95ms != nil || s.LoopZeroAPIRestarts != nil {
		t.Errorf("missing fields must be nil (not 0), got %+v", s)
	}
}
