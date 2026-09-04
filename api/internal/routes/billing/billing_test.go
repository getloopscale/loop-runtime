// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routes/routestest"

	"github.com/jackc/pgx/v5"
)

// fakeBillingPool implements BillingPool for unit tests.
type fakeBillingPool struct {
	plan       string
	customerID *string
	planErr    error
	custErr    error
}

type fakeRow struct {
	target any
	err    error
}

func (r *fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if r.target == nil {
		return pgx.ErrNoRows
	}
	switch v := dest[0].(type) {
	case *string:
		*v = r.target.(string)
	case **string:
		s := r.target.(string)
		*v = &s
	}
	return nil
}

func (p *fakeBillingPool) QueryRow(_ context.Context, sql string, _ ...any) BillingRow {
	if sql == `SELECT plan FROM subscriptions WHERE tenant_id = $1 LIMIT 1` {
		if p.planErr != nil {
			return &fakeRow{err: p.planErr}
		}
		if p.plan == "" {
			return &fakeRow{err: pgx.ErrNoRows}
		}
		return &fakeRow{target: p.plan}
	}
	if sql == `SELECT stripe_customer_id FROM subscriptions WHERE tenant_id = $1 LIMIT 1` {
		if p.custErr != nil {
			return &fakeRow{err: p.custErr}
		}
		if p.customerID == nil {
			return &fakeRow{err: pgx.ErrNoRows}
		}
		return &fakeRow{target: *p.customerID}
	}
	return &fakeRow{err: pgx.ErrNoRows}
}

func TestBilling_Get_HappyPath(t *testing.T) {
	h := NewBillingHandler(&fakeBillingPool{plan: "pro"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp BillingResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Plan != "pro" {
		t.Errorf("want plan=pro, got %q", resp.Plan)
	}
}

func TestBilling_Get_NoSubscription_DefaultsToStarter(t *testing.T) {
	h := NewBillingHandler(&fakeBillingPool{}) // no plan set → ErrNoRows
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp BillingResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Plan != "starter" {
		t.Errorf("want plan=starter (default), got %q", resp.Plan)
	}
}

func TestBilling_Get_Unauthenticated(t *testing.T) {
	h := NewBillingHandler(&fakeBillingPool{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing", nil)
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 for missing auth, got %d", w.Code)
	}
}

// EC-8 of edge-case review: tenant without stripe_customer_id (Starter
// free tier) MUST get 400 NO_STRIPE_CUSTOMER, NOT 500.
func TestBilling_Portal_NoStripeCustomer_Returns400(t *testing.T) {
	h := NewBillingHandler(&fakeBillingPool{}) // customerID nil → ErrNoRows
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/portal", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Portal(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 (NO_STRIPE_CUSTOMER), got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]string
	_ = json.NewDecoder(w.Body).Decode(&body)
	if body["error"] != "NO_STRIPE_CUSTOMER" {
		t.Errorf("want error=NO_STRIPE_CUSTOMER, got %q", body["error"])
	}
}

// TestBilling_Portal_ComEmissor_DevolveSessaoEmitida — o caminho correto.
//
// Este teste afirmava que uma URL montada com o customerID era resposta valida (o
// "legacy fallback"). Por ADR 2026-08-11 ela deixou de ser: sem emissor o engine falha,
// e com emissor devolve o que o emissor deu. A distincao importa porque a URL construida
// PARECE funcionar — e foi assim que ela sobreviveu no app-dev sem ninguem notar.
func TestBilling_Portal_ComEmissor_DevolveSessaoEmitida(t *testing.T) {
	custID := "cus_TestCustomerID"
	const sessaoEmitida = "https://billing.stripe.com/p/session/live_YWNjdF8xTes"

	h := NewBillingHandler(&fakeBillingPool{customerID: &custID}).
		WithPortalSessionCreator(emissorFalso{url: sessaoEmitida}, "https://app.example/billing")

	req := routestest.WithAuth(httptest.NewRequest(http.MethodPost, "/api/v1/billing/portal", nil), routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.Portal(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("com emissor configurado, queria 200; veio %d body=%s", w.Code, w.Body.String())
	}
	var resp PortalResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decodificar resposta: %v", err)
	}
	if resp.URL != sessaoEmitida {
		t.Errorf("a URL tem de ser a que o emissor devolveu, e nao uma montada aqui.\n  queria %q\n  veio   %q",
			sessaoEmitida, resp.URL)
	}
	if strings.Contains(resp.URL, custID) {
		t.Errorf("a sessao emitida nao deve carregar o id de cobranca do cliente: %q", resp.URL)
	}
}

// emissorFalso substitui o SDK do Stripe nos testes: o que importa aqui e que o handler
// devolva o que o emissor deu, nao que ele saiba falar com o Stripe.
type emissorFalso struct{ url string }

func (e emissorFalso) CreateSession(_ context.Context, _, _ string) (string, error) {
	return e.url, nil
}

// fakePortalCreator is a test double for PortalSessionCreator. Records
// the customerID + returnURL it was called with so tests can assert
// the handler passed correct arguments.
type fakePortalCreator struct {
	url        string
	err        error
	gotCust    string
	gotReturn  string
	gotContext context.Context
}

func (f *fakePortalCreator) CreateSession(ctx context.Context, customerID, returnURL string) (string, error) {
	f.gotCust = customerID
	f.gotReturn = returnURL
	f.gotContext = ctx
	if f.err != nil {
		return "", f.err
	}
	return f.url, nil
}

// T2.7: when SDK is wired, handler calls creator.CreateSession with the
// tenant's stripe_customer_id and the configured return URL, returning
// the real one-time-use portal URL.
func TestBilling_Portal_WithSDK_HappyPath(t *testing.T) {
	custID := "cus_TestSDKPath"
	creator := &fakePortalCreator{url: "https://billing.stripe.com/p/session/test_xxx"}
	h := NewBillingHandler(&fakeBillingPool{customerID: &custID}).
		WithPortalSessionCreator(creator, "https://app.loopzero.cloud/billing")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/portal", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Portal(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp PortalResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.URL != "https://billing.stripe.com/p/session/test_xxx" {
		t.Errorf("want real portal URL from SDK, got %q", resp.URL)
	}
	if creator.gotCust != custID {
		t.Errorf("creator received customer %q, want %q", creator.gotCust, custID)
	}
	if creator.gotReturn != "https://app.loopzero.cloud/billing" {
		t.Errorf("creator received return URL %q, want app.loopzero.cloud/billing", creator.gotReturn)
	}
}

// T2.7: SDK errors (Stripe rate limit, invalid key, network) map to
// 502 BILLING_PROVIDER_UNAVAILABLE with product-language message
// (Rule 3: no "Stripe" leak in user-facing string).
func TestBilling_Portal_WithSDK_StripeError_Returns502(t *testing.T) {
	custID := "cus_TestSDKError"
	creator := &fakePortalCreator{err: errSDKFailure}
	h := NewBillingHandler(&fakeBillingPool{customerID: &custID}).
		WithPortalSessionCreator(creator, "https://app.loopzero.cloud/billing")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/portal", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Portal(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("want 502 (BILLING_PROVIDER_UNAVAILABLE), got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]string
	_ = json.NewDecoder(w.Body).Decode(&body)
	if body["error"] != "BILLING_PROVIDER_UNAVAILABLE" {
		t.Errorf("want error=BILLING_PROVIDER_UNAVAILABLE, got %q", body["error"])
	}
	// Rule 3 guard: message must not leak "Stripe" or "stripe" to user.
	msg := body["message"]
	if strings.Contains(msg, "Stripe") || strings.Contains(msg, "stripe") {
		t.Errorf("Rule 3 violation: message leaks internal vendor name: %q", msg)
	}
}

// T2.7: no-Stripe-customer EC-8 path still 400 even with SDK wired —
// SDK only mints session for valid customers; missing customer is a
// front-of-handler check that does NOT call the SDK.
func TestBilling_Portal_NoCustomer_DoesNotCallSDK(t *testing.T) {
	creator := &fakePortalCreator{url: "should-never-be-returned"}
	h := NewBillingHandler(&fakeBillingPool{}). // no customer ID
							WithPortalSessionCreator(creator, "https://app.loopzero.cloud/billing")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/portal", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Portal(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 NO_STRIPE_CUSTOMER, got %d", w.Code)
	}
	if creator.gotCust != "" {
		t.Errorf("SDK was called for customer-less tenant (got customer=%q); should short-circuit", creator.gotCust)
	}
}

// errSDKFailure is a sentinel for SDK error simulation. Defined as
// package-level var so the test reads naturally without inline new.
var errSDKFailure = stripeErr("test sdk failure")

type stripeErr string

func (e stripeErr) Error() string { return string(e) }

// TestBilling_Portal_SemEmissorNaoConstroiURL fecha o B-032.
//
// MEDIDO em 2026-08-11: sem `STRIPE_SECRET_KEY`, o handler caía num caminho que montava
// `https://billing.stripe.com/p/login/` + `customerID` e devolvia isso como se fosse a
// sessão do portal. Não é sessão emitida pelo Stripe — é uma URL construída com o
// identificador de cobrança do cliente dentro, que trafega em histórico de navegador, log
// de proxy e barra de endereços.
//
// E não era hipótese: no app-dev o Secret `loopzero-api-stripe` é `optional: true` e não
// existe, então esse era o caminho ATIVO. O comentário original dizia que o fallback
// servia para "local dev + tests"; a intenção era local, o efeito era público.
//
// A decisão (ADR 2026-08-11) é que o `loopzero-cloud` é o único emissor. Quando o engine não
// pode emitir, ele FALHA dizendo onde ir — uma URL que parece funcionar esconde o problema
// até alguém investigar por que o portal não abriu.
func TestBilling_Portal_SemEmissorNaoConstroiURL(t *testing.T) {
	custID := "cus_TestCustomerID"
	h := NewBillingHandler(&fakeBillingPool{customerID: &custID}) // sem WithPortalSessionCreator

	req := routestest.WithAuth(httptest.NewRequest(http.MethodPost, "/api/v1/billing/portal", nil), routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.Portal(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("sem emissor configurado o engine NÃO pode devolver 200 com uma URL montada — "+
			"ela leva o id de cobrança do cliente no endereço e não é sessão emitida. "+
			"Veio %d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), custID) {
		t.Errorf("a resposta carrega o id de cobrança do cliente (%s): %s", custID, w.Body.String())
	}
}
