// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statushttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"github.com/getloopscale/loop-runtime/api/internal/routestest"
	"github.com/getloopscale/loop-runtime/api/internal/sse"
)

type ownedProject struct{}

func (ownedProject) GetByName(context.Context, string, string) (*ports.ProjectResult, error) {
	return &ports.ProjectResult{Name: "shop"}, nil
}

// signalsByEnv holds signal.changed events per environment and answers
// only the environment it is asked for.
type signalsByEnv struct {
	byEnv map[string][]sse.SSEEvent
	asked []string
}

func (s *signalsByEnv) GetSignalsAfter(_ context.Context, _, _, env string, afterID int64) ([]sse.SSEEvent, error) {
	s.asked = append(s.asked, env)
	var out []sse.SSEEvent
	for _, ev := range s.byEnv[env] {
		if ev.ID > afterID {
			out = append(out, ev)
		}
	}
	return out, nil
}

func signal(id int64, env string) sse.SSEEvent {
	return sse.SSEEvent{ID: id, EventType: "signal.changed", Data: json.RawMessage(`{"env":"` + env + `"}`)}
}

func TestStatusSSE_StreamsOnlyTheRequestedEnvironmentsSignals(t *testing.T) {
	store := &signalsByEnv{byEnv: map[string][]sse.SSEEvent{
		"staging":    {signal(1, "staging")},
		"production": {signal(2, "production")},
	}}
	h := NewStatusSSEHandler(ownedProject{}, store, StatusSSEHandlerOpts{
		PollInterval: time.Millisecond, MaxDuration: 20 * time.Millisecond,
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/shop/signals/stream?env=staging", nil)
	req = routestest.WithChiParam(req, "name", "shop")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	rec := httptest.NewRecorder()

	h.Get(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `"env":"staging"`) {
		t.Fatalf("the staging signal was not streamed: %q", body)
	}
	if strings.Contains(body, `"env":"production"`) {
		t.Fatalf("a client subscribed to staging received a production signal: %q", body)
	}
	for _, env := range store.asked {
		if env != "staging" {
			t.Fatalf("store asked for environment %q, want only staging", env)
		}
	}
}
