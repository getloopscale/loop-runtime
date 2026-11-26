// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"slices"
	"testing"
)

// Every platform token comes from Hydra, RS256 and audience-bound (docs/decisions.md,
// § Identity, #477). A resource server reads the tenant and the account from the token
// itself, and Hydra puts consent-session claims only under `ext` unless they are allowed
// top-level — measured on 2026-09-30, `tenant_id` existed nowhere else. The four claims
// consent injects are therefore promoted in every environment, and still mirrored into
// `ext` while the readers that look there (loop-console's RequireJWT fallback, the
// loop-runtime-api Ext.Email fallback) are migrated.
func TestEveryEnvironmentPromotesTheConsentClaimsTopLevel(t *testing.T) {
	want := []string{"account_id", "email", "name", "tenant_id"}
	for _, env := range declaredPlatformEnvironments(t) {
		t.Run(env, func(t *testing.T) {
			t.Parallel()
			// The drill overlay points Hydra at a managed database and requires its DSN to render.
			values := helmfileValues(t, "core.helmfile.yaml.gotmpl", env, "ory-hydra", append(append([]string{}, helmfileCoreEnv...), "HYDRA_DSN=render-test")...)
			oauth2, _ := dig(values, "hydra", "config", "oauth2").(map[string]any)

			raw, _ := oauth2["allowed_top_level_claims"].([]any)
			var got []string
			for _, c := range raw {
				s, _ := c.(string)
				got = append(got, s)
			}
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Errorf("%s: oauth2.allowed_top_level_claims = %v, want %v", env, got, want)
			}
			if mirror, ok := oauth2["mirror_top_level_claims"].(bool); !ok || !mirror {
				t.Errorf("%s: oauth2.mirror_top_level_claims = %v, want true until the ext readers are gone", env, oauth2["mirror_top_level_claims"])
			}
		})
	}
}
