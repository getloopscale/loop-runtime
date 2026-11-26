// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package dogfood

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCourierMessagesAsksForTheRecipientAndDecodesTheList(t *testing.T) {
	var asked string
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path + "?recipient=" + r.URL.Query().Get("recipient")
		_, _ = w.Write([]byte(`[{"template_type":"verification_code_valid","status":"sent","recipient":"a+b@resend.dev","send_count":1}]`))
	}))
	defer admin.Close()

	messages, err := CourierMessages(context.Background(), admin.URL, "a+b@resend.dev")
	if err != nil {
		t.Fatalf("CourierMessages: %v", err)
	}
	if asked != "/admin/courier/messages?recipient=a+b@resend.dev" {
		t.Fatalf("asked %q; want the courier list filtered by the recipient, with its plus sign intact", asked)
	}
	if len(messages) != 1 || messages[0].TemplateType != "verification_code_valid" || messages[0].Status != "sent" || messages[0].SendCount != 1 {
		t.Fatalf("messages = %+v; want the one sent verification message", messages)
	}
}

func TestCourierMessagesReportsAnAnswerThatIsNotAList(t *testing.T) {
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"down"}`))
	}))
	defer admin.Close()

	if _, err := CourierMessages(context.Background(), admin.URL, "a@resend.dev"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("error = %v; want one that names HTTP 500", err)
	}
}

func TestSinkAddressIsUniqueAndGoesToTheResendSink(t *testing.T) {
	at := time.Unix(1795693010, 42)
	address := SinkAddress("signup", at)
	if address != "delivered+signup-1795693010000000042@resend.dev" {
		t.Fatalf("SinkAddress = %q", address)
	}
	if SinkAddress("signup", at.Add(time.Nanosecond)) == address {
		t.Fatal("two instants gave the same address")
	}
}
