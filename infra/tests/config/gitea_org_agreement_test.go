// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// B-191 -- ONE ORGANISATION, SEVERAL PLACES THAT NAME IT.
//
// B-069 was: the tenant gitops repository was created under one Gitea
// organisation and written to under another. B-184 removed the literal that
// caused it and made the organisation configuration -- which is better, and
// which moves the same failure mode from "a hardcode nobody can change" to "two
// values nobody compares". Before this test the only thing holding the two
// sides together was a doc comment on constants.DefaultGiteaOrg saying they must
// agree.
//
// MEASURED 2026-09-05, and the measurement is why the assertion below is
// equality rather than a documented split:
//
//   - api/internal/config reads GITEA_ORG into cfg.GiteaOrg, and cfg.GiteaOrg
//     reaches exactly three constructors -- the gitops pusher twice
//     (api/cmd/setup_workflows.go, api/cmd/controller/setup_services.go) and the
//     source store once (api/cmd/setup_stores.go). Every repository they address
//     is per-tenant: "{tenantID}-gitops" and "loop-runtime-source-{tenantID}[-{projectID}]".
//   - operators/cmd/tenant-operator reads GITEA_ORG into the provisioner, which
//     EnsureRepo's tenant.Spec.GitOps.RepoName -- and the API itself sets that
//     field to fmt.Sprintf("%s-gitops", t.Name) in
//     api/internal/k8s/tenancy/tenant_cr_manager.go. Same repository, two orgs.
//   - the operator's provisioner return value becomes tenant.Status.GitOpsRepo,
//     which reconcileArgoCDApp uses verbatim as the ArgoCD Application repoURL.
//     So the operator's organisation is the one ArgoCD syncs, and the API's is
//     the one the manifests land in. Disagreement is silent on both sides: the
//     pusher EnsureRepo's its own org, so the push succeeds into a repository
//     nothing watches, and ArgoCD keeps syncing an empty one.
//   - pb.Spec.Gitea.Organizations does NOT select an organisation. It is the
//     create-if-missing list ensureGiteaOrgs walks
//     (operators/internal/platform/platformbootstrap_gitea.go). It cannot be the
//     mechanism by which "the control plane owns its own org" -- there is no such
//     mechanism on this knob.
//
// loop-runtime-platform is NOT created by anything in this tree. The only creator was the
// production bootstrap-platform script, deleted as dead (no caller, #240); nothing
// routes it through GITEA_ORG, and every token-provisioning path ensures only loop-runtime.
//
// WHY THE GO DEFAULTS ARE IN THE COMPARED SET, not just the YAML. There is no
// operator overlay under infra/helm at all, so on that install path the operator
// takes constants.DefaultGiteaOrg while the API takes whatever charts/loop-runtime-api
// declares (the bootstrap chart infra/helm/loop-runtime-api, which declared its own, was
// deleted when the pair became one chart). A non-declaring side still contributes an
// effective organisation, and that is the value its fallback names. Comparing
// declared values alone would let the API overlay drift away from the operator's
// fallback without a single test going red -- which is the state this item was
// filed against.
//
// The consequence is deliberate: an operator who wants a different organisation
// must move every site, Go defaults included. If a genuine per-environment split
// ever has a reason, this test failing is the prompt to write that reason down
// rather than to discover it from an empty ArgoCD sync.

// b191PinnedOrgSites are the declaration sites known to exist when this test was
// written. They are a FLOOR, not the input: discovery below walks charts/ and
// infra/helm/ by pattern so a new chart or overlay is compared without anyone
// remembering to list it. The pin exists so that a rename cannot quietly shrink
// the scan to two sites and leave this test green over a divergence it no longer
// looks at.
var b191PinnedOrgSites = []string{
	"charts/loop-runtime-api/values.yaml",
	"charts/loop-runtime-tenant-operator/values.yaml",
}

// b191OrgLineRE matches a Gitea-organisation declaration in a Helm values file,
// in either spelling: the chart-values key (giteaOrg:) or the env-var key
// (GITEA_ORG:) that the bootstrap overlay uses.
var b191OrgLineRE = regexp.MustCompile(`^\s*(?:GITEA_ORG|giteaOrg):\s*"?([A-Za-z0-9][A-Za-z0-9._-]*)"?\s*(?:#.*)?$`)

type b191Site struct {
	path  string // repo-relative
	line  int
	value string
}

func TestGiteaOrgDeclarationsAgree(t *testing.T) {
	const repoRoot = "../../.."

	// ---- the two Go fallbacks, read from source rather than restated here ----
	apiDefault := b191Extract(t,
		filepath.Join(repoRoot, "api/internal/config/config.go"),
		regexp.MustCompile(`envOr\("GITEA_ORG",\s*"([^"]*)"\)`))
	opDefault := b191Extract(t,
		filepath.Join(repoRoot, "operators/internal/constants/resources.go"),
		regexp.MustCompile(`DefaultGiteaOrg\s*=\s*"([^"]*)"`))

	// ---- discovery: every Helm values file under charts/ and infra/helm/ ----
	var found []b191Site
	for _, root := range []string{"charts", "infra/helm"} {
		abs := filepath.Join(repoRoot, root)
		if _, err := os.Stat(abs); err != nil {
			t.Fatalf("SETUP BROKEN: scan root %s: %v", root, err)
		}
		err := filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			name := d.Name()
			if name != "values.yaml" && (!strings.HasPrefix(name, "values-") || !strings.HasSuffix(name, ".yaml")) {
				return nil
			}
			data, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			rel, rerr := filepath.Rel(repoRoot, p)
			if rerr != nil {
				return rerr
			}
			for i, ln := range strings.Split(string(data), "\n") {
				if m := b191OrgLineRE.FindStringSubmatch(ln); m != nil {
					found = append(found, b191Site{path: filepath.ToSlash(rel), line: i + 1, value: m[1]})
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("SETUP BROKEN: walking %s: %v", root, err)
		}
	}

	// ---- floors: a scan that found nothing must not be reported as agreement ----
	if len(found) < len(b191PinnedOrgSites) {
		t.Fatalf("SETUP BROKEN: discovery found %d Gitea-org declaration(s), fewer than the %d pinned. "+
			"Either the pattern stopped matching the values files or the sites moved; a shrunken scan "+
			"agrees with itself over a divergence it no longer reads. Found: %v",
			len(found), len(b191PinnedOrgSites), b191Describe(found))
	}
	for _, pinned := range b191PinnedOrgSites {
		hit := false
		for _, s := range found {
			if s.path == pinned {
				hit = true
				break
			}
		}
		if !hit {
			t.Fatalf("SETUP BROKEN: pinned declaration site %s was not re-encountered. Either it moved "+
				"and this pin is stale prose, or the scan stopped seeing it. Found: %v",
				pinned, b191Describe(found))
		}
	}

	// ---- the assertion: one organisation, however many places name it ----
	byValue := map[string][]string{}
	byValue[apiDefault] = append(byValue[apiDefault],
		"api/internal/config/config.go (envOr GITEA_ORG fallback)")
	byValue[opDefault] = append(byValue[opDefault],
		"operators/internal/constants/resources.go (DefaultGiteaOrg)")
	for _, s := range found {
		byValue[s.value] = append(byValue[s.value], fmt.Sprintf("%s:%d", s.path, s.line))
	}
	if len(byValue) == 1 {
		return
	}

	values := make([]string, 0, len(byValue))
	for v := range byValue {
		values = append(values, v)
	}
	sort.Strings(values)
	var b strings.Builder
	fmt.Fprintf(&b, "B-191 regression: the Gitea organisation is named %d different ways. "+
		"The tenant operator provisions {tenant}-gitops in its organisation and hands that URL to "+
		"ArgoCD; the API pushes the manifests into its own. They must be the same organisation "+
		"(B-069). Sites:", len(byValue))
	for _, v := range values {
		sites := byValue[v]
		sort.Strings(sites)
		fmt.Fprintf(&b, "\n  %q -> %s", v, strings.Join(sites, ", "))
	}
	t.Error(b.String())
}

func b191Extract(t *testing.T, path string, re *regexp.Regexp) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("SETUP BROKEN: read %s: %v", path, err)
	}
	m := re.FindStringSubmatch(string(data))
	if m == nil {
		t.Fatalf("SETUP BROKEN: %s no longer contains a Gitea-org default matching %s. "+
			"A fallback this test cannot find is a fallback it cannot compare.", path, re)
	}
	if m[1] == "" {
		t.Fatalf("SETUP BROKEN: %s declares an EMPTY Gitea-org default; an empty organisation "+
			"is not a value any side can agree with.", path)
	}
	return m[1]
}

func b191Describe(sites []b191Site) []string {
	out := make([]string, 0, len(sites))
	for _, s := range sites {
		out = append(out, fmt.Sprintf("%s:%d=%q", s.path, s.line, s.value))
	}
	sort.Strings(out)
	return out
}
