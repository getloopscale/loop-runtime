// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package billing implements Stripe integration for subscription management.
package billing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// StripeClient communicates with the Stripe API.
type StripeClient struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
}

// NewStripeClient creates a Stripe API client.
func NewStripeClient(apiKey string) *StripeClient {
	return NewStripeClientWithURL(apiKey, "https://api.stripe.com/v1")
}

// NewStripeClientWithURL creates a Stripe API client with a custom base URL (for testing).
func NewStripeClientWithURL(apiKey, baseURL string) *StripeClient {
	return &StripeClient{
		apiKey:  apiKey,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Customer represents a Stripe customer.
type Customer struct {
	ID    string            `json:"id"`
	Email string            `json:"email"`
	Name  string            `json:"name"`
	Meta  map[string]string `json:"metadata"`
}

// Subscription represents a Stripe subscription.
type Subscription struct {
	ID                 string `json:"id"`
	CustomerID         string `json:"customer"`
	Status             string `json:"status"`
	CurrentPeriodStart int64  `json:"current_period_start"`
	CurrentPeriodEnd   int64  `json:"current_period_end"`
}

// PortalSession represents a Stripe Customer Portal session.
type PortalSession struct {
	URL string `json:"url"`
}

// CreateCustomer creates a Stripe customer for a new tenant.
func (c *StripeClient) CreateCustomer(email, tenantID, name string) (*Customer, error) {
	data := url.Values{
		"email":               {email},
		"name":                {name},
		"metadata[tenant_id]": {tenantID},
	}

	// Idempotency key prevents duplicate customers on crash/retry
	idempotencyKey := fmt.Sprintf("%s-create-customer", tenantID)

	var customer Customer
	if err := c.post("/customers", data, &customer, idempotencyKey); err != nil {
		return nil, fmt.Errorf("create customer: %w", err)
	}
	return &customer, nil
}

// CreateSubscription creates a subscription for a customer.
func (c *StripeClient) CreateSubscription(customerID, priceID, tenantID string) (*Subscription, error) {
	data := url.Values{
		"customer":               {customerID},
		"items[0][price]":        {priceID},
		"payment_behavior":       {"default_incomplete"},
		"payment_settings[save_default_payment_method]": {"on_subscription"},
	}

	idempotencyKey := fmt.Sprintf("%s-create-subscription", tenantID)

	var sub Subscription
	if err := c.post("/subscriptions", data, &sub, idempotencyKey); err != nil {
		return nil, fmt.Errorf("create subscription: %w", err)
	}
	return &sub, nil
}

// CreatePortalSession creates a Customer Portal session URL.
func (c *StripeClient) CreatePortalSession(customerID, returnURL string) (*PortalSession, error) {
	data := url.Values{
		"customer":   {customerID},
		"return_url": {returnURL},
	}

	var session PortalSession
	if err := c.post("/billing_portal/sessions", data, &session, ""); err != nil {
		return nil, fmt.Errorf("create portal session: %w", err)
	}
	return &session, nil
}

// ReportUsage reports metered usage for a subscription item.
func (c *StripeClient) ReportUsage(subscriptionItemID string, quantity int64, timestamp int64) error {
	data := url.Values{
		"quantity":  {fmt.Sprintf("%d", quantity)},
		"timestamp": {fmt.Sprintf("%d", timestamp)},
		"action":   {"increment"},
	}

	path := fmt.Sprintf("/subscription_items/%s/usage_records", subscriptionItemID)
	return c.post(path, data, nil, "")
}

func (c *StripeClient) post(path string, data url.Values, result interface{}, idempotencyKey string) error {
	req, err := http.NewRequest("POST", c.baseURL+path, bytes.NewBufferString(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		// Don't leak Stripe error details — they may contain account info.
		// Raw body is available for debug logging by the caller.
		return fmt.Errorf("payment service error (status %d)", resp.StatusCode)
	}

	if result != nil {
		return json.Unmarshal(body, result)
	}
	return nil
}
