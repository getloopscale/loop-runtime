package domains

import (
	"strings"
	"testing"
)

// The customer creates this record in their own DNS, so it carries the product's name and
// no name of a component (getloopscale/loop-console#633 follow-up, decided on 2026-11-27).
func TestVerificationRecordCarriesTheProductName(t *testing.T) {
	if got, want := VerificationTXTName("api.acme.com"), "_loopscale-verify.api.acme.com"; got != want {
		t.Errorf("VerificationTXTName = %q, want %q", got, want)
	}
	if !strings.HasPrefix(VerificationTXTValue("0a1b"), "loopscale-verify=") {
		t.Errorf("VerificationTXTValue = %q, want the loopscale-verify= prefix", VerificationTXTValue("0a1b"))
	}
}
