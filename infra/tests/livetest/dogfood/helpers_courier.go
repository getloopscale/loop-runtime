// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package dogfood

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

// KratosAdminEnv names the variable with the base URL of the Kratos admin API.
// That API is never exposed: whoever runs the suite reaches it through a
// port-forward and hands the local address here.
const KratosAdminEnv = "LOOP_RUNTIME_DOGFOOD_KRATOS_ADMIN_URL"

// KratosAdminURL is the Kratos admin API of the environment, "" when the run
// has none.
func KratosAdminURL() string { return os.Getenv(KratosAdminEnv) }

// CourierMessage is one message Kratos handed, or will hand, to the mail
// provider. Kratos keeps no readable copy of the code a message carries, so a
// spec can tell that a mail went out but cannot use what it says.
type CourierMessage struct {
	TemplateType string `json:"template_type"`
	Status       string `json:"status"`
	Recipient    string `json:"recipient"`
	SendCount    int    `json:"send_count"`
}

// CourierMessages lists the messages Kratos has for one recipient, newest
// first.
func CourierMessages(ctx context.Context, adminURL, recipient string) ([]CourierMessage, error) {
	query := url.Values{"recipient": {recipient}, "page_size": {"20"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, adminURL+"/admin/courier/messages?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("list the courier messages: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read the courier messages: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the courier list answered HTTP %d", resp.StatusCode)
	}
	var messages []CourierMessage
	if err := json.Unmarshal(raw, &messages); err != nil {
		return nil, fmt.Errorf("decode the courier messages: %w", err)
	}
	return messages, nil
}

// SinkAddress is a new address at Resend's delivery sink: the provider accepts
// the mail and delivers it nowhere, so a spec sends real mail without a mailbox
// filling up or a bounce counting against the sending domain.
func SinkAddress(label string, at time.Time) string {
	return fmt.Sprintf("delivered+%s-%d@resend.dev", label, at.UnixNano())
}

// SignUp registers a new identity through the browser registration flow and
// returns the browser that holds its session.
func SignUp(ctx context.Context, env Env, email, password string) (*Browser, Response, error) {
	browser, err := NewBrowser(env, "")
	if err != nil {
		return nil, Response{}, err
	}
	started, err := browser.Do(ctx, http.MethodGet, env.AuthBaseURL+"/self-service/registration/browser", "application/json", nil, false)
	if err != nil {
		return nil, Response{}, err
	}
	if started.Status != http.StatusOK {
		return nil, started, fmt.Errorf("the registration flow answered HTTP %d", started.Status)
	}
	flow := started.JSON()
	answer, err := browser.Do(ctx, http.MethodPost, FlowAction(flow), "application/json", map[string]any{
		"method":     "password",
		"password":   password,
		"csrf_token": FlowField(flow, "csrf_token"),
		"traits":     map[string]any{"email": email, "name": "Staging Journey"},
	}, false)
	return browser, answer, err
}

// NewPassword is a random password for an identity a spec creates and never
// signs in to again.
func NewPassword() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// SentTo reports whether Kratos handed the provider a message of the template
// to the recipient.
func SentTo(ctx context.Context, adminURL, recipient, template string) (bool, error) {
	messages, err := CourierMessages(ctx, adminURL, recipient)
	if err != nil {
		return false, err
	}
	for _, message := range messages {
		if message.TemplateType == template && message.Status == "sent" {
			return true, nil
		}
	}
	return false, nil
}
