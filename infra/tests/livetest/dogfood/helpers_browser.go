// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package dogfood

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"
)

// SessionPathEnv names the file that holds the validation identity's Kratos
// session, the value of its ory_kratos_session cookie. infra/staging/validate.sh
// writes it when it seeds the identity.
const SessionPathEnv = "LOOPZERO_DOGFOOD_SESSION_PATH"

// LoadSession reads the Kratos session the browser journeys sign in with.
func LoadSession() (string, error) {
	path := os.Getenv(SessionPathEnv)
	if path == "" {
		return "", fmt.Errorf("%s is not set: the journey needs the file with the validation identity's Kratos session", SessionPathEnv)
	}
	raw, err := os.ReadFile(path) //nolint:gosec // G304: the path comes from the operator's environment
	if err != nil {
		return "", fmt.Errorf("read the Kratos session: %w", err)
	}
	session := strings.TrimSpace(string(raw))
	if session == "" {
		return "", fmt.Errorf("the Kratos session file %s is empty", path)
	}
	return session, nil
}

// Browser is an HTTP client that behaves as a browser does in the auth flows:
// it keeps cookies and hands back each redirect, one hop at a time. With a
// session it is signed in as the validation identity.
type Browser struct {
	client  *http.Client
	session string
	// hosts are the only ones a request may go to, so that the session and the
	// flow cookies never leave the environment under test.
	hosts map[string]bool
}

// NewBrowser returns a browser for env. session may be "" for a signed-out one.
func NewBrowser(env Env, session string) (*Browser, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	hosts := map[string]bool{}
	for _, base := range []string{env.AuthBaseURL, env.CloudBaseURL, env.AppBaseURL} {
		parsed, err := url.Parse(base)
		if err != nil {
			return nil, fmt.Errorf("parse %q: %w", base, err)
		}
		hosts[parsed.Host] = true
	}
	return &Browser{
		client: &http.Client{
			Timeout:       30 * time.Second,
			Jar:           jar,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		session: session,
		hosts:   hosts,
	}, nil
}

// Response is what one request answered: the redirect target is absolute, and
// empty when the answer is not a redirect.
type Response struct {
	Status   int
	Location string
	Body     []byte
}

// JSON decodes the body into a map; a body that is not JSON gives an empty one.
func (r Response) JSON() map[string]any {
	out := map[string]any{}
	_ = json.Unmarshal(r.Body, &out)
	return out
}

// Do sends one request and does not follow its redirect. A form body is sent
// urlencoded when form is true, as JSON otherwise.
func (b *Browser) Do(ctx context.Context, method, target, accept string, payload any, form bool) (Response, error) {
	parsed, err := url.Parse(target)
	if err != nil {
		return Response{}, fmt.Errorf("parse %q: %w", target, err)
	}
	if !b.hosts[parsed.Host] {
		return Response{}, fmt.Errorf("refusing to send a request to %s: it is outside the environment under test", parsed.Host)
	}
	var body io.Reader
	contentType := ""
	switch {
	case payload == nil:
	case form:
		values, ok := payload.(url.Values)
		if !ok {
			return Response{}, fmt.Errorf("a form body must be url.Values")
		}
		body, contentType = strings.NewReader(values.Encode()), "application/x-www-form-urlencoded"
	default:
		encoded, err := json.Marshal(payload)
		if err != nil {
			return Response{}, err
		}
		body, contentType = strings.NewReader(string(encoded)), "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Accept", accept)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if b.session != "" {
		req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: b.session})
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("%s %s%s: %w", method, parsed.Host, parsed.Path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Response{}, fmt.Errorf("read the answer of %s %s%s: %w", method, parsed.Host, parsed.Path, err)
	}
	out := Response{Status: resp.StatusCode, Body: raw}
	if location := resp.Header.Get("Location"); location != "" {
		next, err := parsed.Parse(location)
		if err != nil {
			return Response{}, fmt.Errorf("parse the redirect of %s%s: %w", parsed.Host, parsed.Path, err)
		}
		out.Location = next.String()
	}
	return out, nil
}

// FlowField returns the value of the named input of a Kratos flow, "" when
// the flow has no such input.
func FlowField(flow map[string]any, name string) string {
	ui, _ := flow["ui"].(map[string]any)
	nodes, _ := ui["nodes"].([]any)
	for _, raw := range nodes {
		node, _ := raw.(map[string]any)
		attributes, _ := node["attributes"].(map[string]any)
		if attributes["name"] == name {
			value, _ := attributes["value"].(string)
			return value
		}
	}
	return ""
}

// FlowAction is the URL a Kratos flow is submitted to.
func FlowAction(flow map[string]any) string {
	ui, _ := flow["ui"].(map[string]any)
	action, _ := ui["action"].(string)
	return action
}
