// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package sse

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func openTestStream(t *testing.T) (*Stream, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	s, err := Open(rec)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return s, rec
}

func fastLoop() Loop { return Loop{Interval: time.Millisecond} }

func TestGate_RefusesTheStreamPastItsLimit(t *testing.T) {
	g := NewGate(1)
	leave, ok := g.Enter()
	if !ok {
		t.Fatal("first stream refused")
	}
	if _, ok := g.Enter(); ok {
		t.Fatal("second stream admitted past a limit of one")
	}
	leave()
	if _, ok := g.Enter(); !ok {
		t.Fatal("a freed slot was not reusable")
	}
}

func TestOpen_RefusesAWriterThatCannotFlush(t *testing.T) {
	if _, err := Open(struct{ http.ResponseWriter }{httptest.NewRecorder()}); !errors.Is(err, ErrStreamingUnsupported) {
		t.Fatalf("want ErrStreamingUnsupported, got %v", err)
	}
}

func TestLoop_EndsWhenThePollReportsDone(t *testing.T) {
	s, _ := openTestStream(t)
	calls := 0
	err := fastLoop().Run(context.Background(), s, func(context.Context) (bool, error) {
		calls++
		return calls == 3, nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("want nil after 3 polls, got err=%v calls=%d", err, calls)
	}
}

func TestLoop_GivesUpAfterConsecutiveSourceFailures(t *testing.T) {
	s, _ := openTestStream(t)
	calls := 0
	err := Loop{Interval: time.Millisecond, MaxConsecutiveErrors: 3}.Run(context.Background(), s,
		func(context.Context) (bool, error) {
			calls++
			return false, errors.New("db down")
		})
	if !errors.Is(err, ErrPollRetriesExhausted) || calls != 3 {
		t.Fatalf("want ErrPollRetriesExhausted after 3 attempts, got err=%v calls=%d", err, calls)
	}
}

func TestLoop_ASuccessResetsTheFailureCount(t *testing.T) {
	s, _ := openTestStream(t)
	calls := 0
	err := Loop{Interval: time.Millisecond, MaxConsecutiveErrors: 2}.Run(context.Background(), s,
		func(context.Context) (bool, error) {
			calls++
			switch calls {
			case 1, 3: // failure, success, failure, success...
				return false, errors.New("transient")
			case 5:
				return true, nil
			}
			return false, nil
		})
	if err != nil {
		t.Fatalf("interleaved single failures must not exhaust the budget: %v", err)
	}
}

func TestLoop_StopsAtOnceWhenTheClientIsGone(t *testing.T) {
	s, _ := openTestStream(t)
	calls := 0
	err := fastLoop().Run(context.Background(), s, func(context.Context) (bool, error) {
		calls++
		return true, ErrClientGone
	})
	if !errors.Is(err, ErrClientGone) || calls != 1 {
		t.Fatalf("want ErrClientGone after one poll, got err=%v calls=%d", err, calls)
	}
}

func TestLoop_EndsCleanlyWhenTheContextEnds(t *testing.T) {
	s, _ := openTestStream(t)
	ctx, cancel := context.WithCancel(context.Background())
	err := fastLoop().Run(ctx, s, func(context.Context) (bool, error) {
		cancel()
		return false, errors.New("cancelled mid-query")
	})
	if err != nil {
		t.Fatalf("a cancelled context is a normal end, got %v", err)
	}
}

func TestLoop_SendsHeartbeats(t *testing.T) {
	s, rec := openTestStream(t)
	calls := 0
	_ = Loop{Interval: 20 * time.Millisecond, Heartbeat: time.Millisecond}.Run(context.Background(), s,
		func(context.Context) (bool, error) {
			calls++
			return calls == 2, nil
		})
	if !strings.Contains(rec.Body.String(), ":heartbeat") {
		t.Fatalf("no heartbeat written: %q", rec.Body.String())
	}
}

func TestStream_ControlEventsCarryNoResumeID(t *testing.T) {
	s, rec := openTestStream(t)
	if err := s.Send("", "stream.done", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.Body.String(), "id:") {
		t.Fatalf("a control event must not carry an id: %q", rec.Body.String())
	}
}

func TestLastEventID_IgnoresAnUnparseableHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Last-Event-ID", "not-a-number")
	if got := LastEventID(req); got != 0 {
		t.Fatalf("want 0, got %d", got)
	}
}
