// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package config — regression suite for B1-B18 (CHG-2026-04-28-LAYERED-BOOTSTRAP T8.1).
//
// Each test pre-condition mirrors a documented L3 bug. The suite exists so
// CI fails early if any of those bugs reappear via configuration drift.
//
// Run:
//
//	LOOPZERO_REGRESSION=1 go test ./infra/tests/config/ -run TestRegression -v
package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// runRegressionScript invokes regression-checks.sh and returns combined output.
func TestB50LoopZeroCloudPort13002(t *testing.T) {
	data, err := os.ReadFile("../../../charts/loopzero-cloud/values.yaml")
	if err != nil {
		t.Fatalf("read loopzero-cloud values.yaml: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "port: 13002") {
		t.Errorf("B50 regression: loopzero-cloud service.port must be 13002 (matches binary :13002)")
	}
	// The probes are values of the one loopzero-cloud chart, rendered by its
	// Deployment template; the bootstrap copy that spelled them in the
	// template was deleted when the pair became one chart.
	if !strings.Contains(s, "path: /health") {
		t.Errorf("B50 regression: loopzero-cloud probes must use /health (not /)")
	}
}

func TestB59BuildClusterPortNotConflicting(t *testing.T) {
	data, err := os.ReadFile("../../k3d/build-cluster.yaml")
	if err != nil {
		t.Fatalf("read build-cluster.yaml: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "port: 30193:30093") {
		t.Errorf("B59 regression: build cluster must use external 30193 (mgmt already binds 30093)")
	}
	if !strings.Contains(s, "port: 30194:30094") {
		t.Errorf("B59 regression: build cluster must use external 30194 (mgmt already binds 30094)")
	}
}

func TestB60PromtailReadinessTcpProbe(t *testing.T) {
	// Path drift fix 2026-05-15 iter 5: per `ef7a7c16` (Loki cross-
	// cluster URL fix), promtail values are now `.gotmpl`-templated per
	// environment so the canonical file is `values.yaml.gotmpl`, not
	// `values.yaml`. See finding `concepts/operations/finding-2026-05-15-
	// infra-tests-load-regression-obsolete.md` and CLAUDE.md "Known
	// issues" Loki cross-cluster entry.
	data, err := os.ReadFile("../../helm/promtail/values.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read promtail values.yaml.gotmpl: %v", err)
	}
	s := string(data)
	idx := strings.Index(s, "readinessProbe:")
	if idx < 0 {
		t.Error("B60 regression: promtail values must override readinessProbe")
	}
	window := s[idx:min(idx+400, len(s))]
	if !strings.Contains(window, "tcpSocket:") {
		t.Errorf("B60 regression: readinessProbe must be tcpSocket")
	}
	if !strings.Contains(window, "httpGet: null") {
		t.Errorf("B60 regression: readinessProbe must explicitly null out chart httpGet")
	}
}

func TestB61CloudflaredStandaloneFallback(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/scripts/generate-runtime-secrets.sh")
	if err != nil {
		t.Fatalf("read generate-runtime-secrets.sh: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "B61: standalone fallback") {
		t.Errorf("B61 regression: generate-runtime-secrets.sh missing standalone fallback marker")
	}
	if !strings.Contains(s, "STANDALONE_LOCAL_NO_CF_TUNNEL") {
		t.Errorf("B61 regression: sentinel token absent")
	}
	rt, err := os.ReadFile("../../helmfile/cell.helmfile.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read runtime helmfile: %v", err)
	}
	if !strings.Contains(string(rt), "replicaCount: 0") {
		t.Errorf("B61 regression: cloudflared replicaCount:0 missing")
	}
}

func TestB62MetricsServerInstalledFalseInRuntime(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/cell.helmfile.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read runtime helmfile: %v", err)
	}
	if !strings.Contains(string(data), `installed: {{ .Values | get "installMetricsServer" false }}`) {
		t.Errorf("B62 regression: runtime metrics-server release must default to installed:false")
	}
}

func TestB63KedaHttpAddonHasNeedsKeda(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/cell.helmfile.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read runtime helmfile: %v", err)
	}
	s := string(data)
	idx := strings.Index(s, "name: keda-http-addon")
	if idx < 0 {
		t.Fatal("B63: keda-http-addon block not found")
	}
	end := strings.Index(s[idx+10:], "- name:")
	if end < 0 {
		end = len(s) - idx - 10
	}
	block := s[idx : idx+10+end]
	if !strings.Contains(block, "- keda/keda") {
		t.Errorf("B63 regression: keda-http-addon must declare needs: keda/keda")
	}
}

func TestB64AdmissionPoliciesAlertsConditional(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/cell.helmfile.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read runtime helmfile: %v", err)
	}
	s := string(data)
	// Accept either direct .Values access or `get` with default (current form
	// is `(.Values | get "clusterTopology" "single-cluster")`).
	if !strings.Contains(s, `clusterTopology`) || !strings.Contains(s, `"multi-cluster"`) {
		t.Errorf("B64 regression: clusterTopology conditional missing")
	}
	idx := strings.Index(s, "name: admission-policies")
	if idx < 0 {
		t.Fatal("B64: admission-policies block not found")
	}
	end := strings.Index(s[idx+10:], "- name:")
	if end < 0 {
		end = len(s) - idx - 10
	}
	block := s[idx : idx+10+end]
	if !strings.Contains(block, "alerts:") || !strings.Contains(block, "enabled: false") {
		t.Errorf("B64 regression: admission-policies must disable alerts in standalone-multi")
	}
}

func TestB77LoopZeroApiGiteaOrgConfigurable(t *testing.T) {
	// The API and the controller build their GitOps pusher through one
	// constructor, backgroundloops.NewGitOpsPusher; the org is set there.
	files := []string{
		"../../../api/internal/backgroundloops/clients.go",
		"../../../api/cmd/setup_stores.go",
	}
	for _, root := range []string{"../../../api/cmd/setup_workflows.go", "../../../api/cmd/controller/setup_services.go"} {
		data, err := os.ReadFile(root)
		if err != nil {
			t.Fatalf("read %s: %v", root, err)
		}
		if !strings.Contains(string(data), "backgroundloops.NewGitOpsPusher(") {
			t.Errorf("B77 regression: %s must build its GitOps pusher through backgroundloops.NewGitOpsPusher, where the org comes from cfg.GiteaOrg", root)
		}
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		s := string(data)
		if strings.Contains(s, `OrgName: "loopzero",`) {
			t.Errorf("B77 regression: %s has OrgName hardcoded to \"loopzero\" — must use cfg.GiteaOrg", f)
		}
		if !strings.Contains(s, "cfg.GiteaOrg") {
			t.Errorf("B77 regression: %s must reference cfg.GiteaOrg (configurable via GITEA_ORG env var)", f)
		}
	}
	// Config struct must declare GiteaOrg + read GITEA_ORG env var
	cfgData, err := os.ReadFile("../../../api/internal/config/config.go")
	if err != nil {
		t.Fatalf("read config.go: %v", err)
	}
	cfg := string(cfgData)
	if !strings.Contains(cfg, "GiteaOrg") {
		t.Error("B77 regression: Config struct must declare GiteaOrg field")
	}
	if !strings.Contains(cfg, `envOr("GITEA_ORG"`) {
		t.Error("B77 regression: config must read GITEA_ORG env var")
	}
}

func TestB78LoopZeroApiHasGiteaTokenEnv(t *testing.T) {
	data, err := os.ReadFile("../../helm/loopzero-api/templates/deployment.yaml")
	if err != nil {
		t.Fatalf("read loopzero-api deployment.yaml: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "name: GITEA_TOKEN") {
		t.Error("B78 regression: loopzero-api Deployment must export env GITEA_TOKEN")
	}
	if !strings.Contains(s, "name: gitea-admin") {
		t.Error("B78 regression: GITEA_TOKEN must come from gitea-admin Secret")
	}
}

func TestB79LoopZeroApiHasTmpVolume(t *testing.T) {
	data, err := os.ReadFile("../../helm/loopzero-api/templates/deployment.yaml")
	if err != nil {
		t.Fatalf("read loopzero-api deployment.yaml: %v", err)
	}
	s := string(data)
	// Must have a `name: tmp` volume with emptyDir
	if !strings.Contains(s, "name: tmp") {
		t.Error("B79 regression: loopzero-api Deployment must have a `tmp` volume")
	}
	if !strings.Contains(s, "emptyDir:") {
		t.Error("B79 regression: tmp volume must be emptyDir (writable)")
	}
	if !strings.Contains(s, "mountPath: /tmp") {
		t.Error("B79 regression: tmp volume must be mounted at /tmp")
	}
}

func TestB75MinioBucketsCompleteness(t *testing.T) {
	required := []string{"loopzero-builds", "loopzero-artifacts", "loopzero-logs", "loopzero-static-assets"}
	for _, f := range []string{
		"../../helm/minio/values.yaml",
		"../../helm/minio/values-distributed.yaml",
	} {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		s := string(data)
		for _, b := range required {
			if !strings.Contains(s, "name: "+b) {
				t.Errorf("B75 regression: %s missing required bucket %q", f, b)
			}
		}
	}
	// Cross-reference: loopzero-static-assets must be named in the argo constants.
	//
	// The path is SEARCHED, not hardcoded. It used to read
	// `../../../api/internal/argo/constants.go` literally; commit 698fd507d moved
	// the four build packages under a parent on 2026-08-18, the read started
	// failing, and `t.Fatalf` sits one line above the assertion — so this
	// cross-reference went unasserted for 18 days while the test reported red for
	// a reason that had nothing to do with B-75. A real regression would have been
	// indistinguishable from the breakage.
	//
	// Zero hits and several hits fail differently on purpose: zero means the file
	// was renamed away and this guard is inert, several means it no longer knows
	// which file it is asserting about. Both are worth different fixes.
	var argoConstants []string
	walkErr := filepath.WalkDir("../../../api", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable subtree is not this test's business
		}
		if d.IsDir() || d.Name() != "constants.go" {
			return nil
		}
		if filepath.Base(filepath.Dir(path)) == "argo" {
			argoConstants = append(argoConstants, path)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk api/ for the argo constants: %v", walkErr)
	}
	switch len(argoConstants) {
	case 1:
		// the expected shape
	case 0:
		t.Fatalf("no constants.go under a directory named argo anywhere in api/ — " +
			"the B75 cross-reference has nothing to assert against, which is a guard " +
			"that stopped guarding rather than an invariant that held")
	default:
		t.Fatalf("%d candidates for the argo constants (%v) — this guard asserts "+
			"against one file and no longer knows which", len(argoConstants), argoConstants)
	}
	apiData, err := os.ReadFile(argoConstants[0]) // #nosec G304 -- path comes from the walk above
	if err != nil {
		t.Fatalf("read %s: %v", argoConstants[0], err)
	}
	if !strings.Contains(string(apiData), `"loopzero-static-assets"`) {
		t.Error("B75 regression: api code must reference loopzero-static-assets bucket name")
	}
}

func TestB71BuildHelmfileHasCWTs(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/build.helmfile.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read build helmfile: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "name: loopzero-build-cwts") {
		t.Error("B71 regression: build helmfile must declare loopzero-build-cwts release")
	}
	if !strings.Contains(s, "../../charts/loopzero-build-cwts") {
		t.Error("B71 regression: loopzero-build-cwts release must reference charts/loopzero-build-cwts")
	}
	// Must depend on argo-workflows (CRDs prerequisite)
	idx := strings.Index(s, "name: loopzero-build-cwts")
	end := strings.Index(s[idx:], "- name:")
	if end < 0 {
		end = len(s) - idx
	}
	block := s[idx : idx+end]
	if !strings.Contains(block, "argo/argo-workflows") {
		t.Error("B71 regression: loopzero-build-cwts must declare needs: argo/argo-workflows")
	}
}

// TestB69GiteaOrgConfigurable asserts B-069: the Gitea organisation that owns
// tenant gitops repositories is configuration (GITEA_ORG), not a literal.
//
// B-184 -- WHY THIS ASSERTED NOTHING, AND WHAT REVIVING IT FOUND. It opened with
// a BARE t.Skip and no guard:
//
//	t.Skip("monolith operators/cmd/main.go deleted T1.4 -- Gitea org env covered
//	       by per-split-binary main.go (post operator-split)")
//	data, err := os.ReadFile("../../../operators/cmd/main.go")
//
// so the six statements below it had not run since T1.4. The reason was wrong in
// the way that matters: the hardcode was not covered by the split, it MOVED with
// the provisioner into operators/cmd/tenant-operator/main.go and stayed there.
// Re-pointing this test at that file found it verbatim --
// gitea.NewRealProvisioner(giteaURL, giteaToken, "loopzero") -- while the API side
// had read envOr("GITEA_ORG", "loopzero") the whole time. A deployment that set the
// org got its repos CREATED in one organisation and PUSHED to another.
//
// MEASURED 2026-09-05: operators/cmd/main.go is absent from this tree, as are
// both chart paths this test used to read (infra/helm/tenant-controller/ and
// infra/helm/loopzero-controller/), so dropping the skip alone would have failed at
// the read rather than reviving anything. The assertions are re-pointed at what
// owns the contract today: tenant-operator is the only split binary that
// CONSTRUCTS the Gitea provisioner, and charts/loopzero-tenant-operator is what
// deploys it.
//
// "Constructs", not "imports". An audit measured the earlier wording false on
// 2026-09-05: `go list -deps ./cmd/platform-operator` resolves
// operators/internal/gitea too, through operators/internal/platform. The
// conclusion survives — NewRealProvisioner is called only in
// tenant-operator/main.go, and the platform path takes its organisations from
// pb.Spec.Gitea.Organizations rather than a literal — but the premise did not, and
// a right conclusion through a false premise exonerates the wrong change next time.
func TestB69GiteaOrgConfigurable(t *testing.T) {
	const mainPath = "../../../operators/cmd/tenant-operator/main.go"
	data, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("read %s: %v", mainPath, err)
	}
	s := string(data)
	// Assert the CALL SHAPE, not the absence of one literal. The assertion this
	// test inherited was `!strings.Contains(s, "...NewRealProvisioner(giteaURL,
	// giteaToken, \"loopzero\")")`, which any other literal walks past -- hardcoding
	// "acme" would have satisfied it. Requiring the variable in the third position
	// is the same correction a mutation run forced on the ADR-0018 metric
	// assertions in regression_adr_test.go: check the declaration, not a mention.
	// Since 4e6f524e0 the operator loads its settings once into a config struct
	// (config.go), so the org reaches the provisioner as cfg.GiteaOrg.
	if !strings.Contains(s, "gitea.NewRealProvisioner(cfg.GiteaURL, cfg.GiteaToken, cfg.GiteaOrg)") {
		t.Error("B69 regression: the Gitea provisioner's orgName must be the configured " +
			"giteaOrg variable, not a string literal")
	}
	cfgData, err := os.ReadFile("../../../operators/cmd/tenant-operator/config.go")
	if err != nil {
		t.Fatalf("read tenant-operator config.go: %v", err)
	}
	if !regexp.MustCompile(`GiteaOrg:\s+orDefault\("GITEA_ORG",`).Match(cfgData) {
		t.Error("B69 regression: the tenant operator's config must load GiteaOrg from the GITEA_ORG env var")
	}
	// Reading the env is only half of the contract: a key the chart never sets is
	// not configurable in any real install, which is what the (now absent)
	// controller-chart half of this test was for.
	for _, c := range []struct{ path, want, why string }{
		{"../../../charts/loopzero-tenant-operator/templates/configmap.yaml", "GITEA_ORG:", "ConfigMap must carry the GITEA_ORG key"},
		{"../../../charts/loopzero-tenant-operator/values.yaml", "giteaOrg:", "values.yaml must expose config.giteaOrg"},
	} {
		d, err := os.ReadFile(c.path)
		if err != nil {
			t.Fatalf("read %s: %v", c.path, err)
		}
		if !strings.Contains(string(d), c.want) {
			t.Errorf("B69 regression: %s: %s (want %q)", c.path, c.why, c.want)
		}
	}
}

func TestB80GiteaCredentialsTemplate(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/scripts/generate-secrets.sh")
	if err != nil {
		t.Fatalf("read generate-secrets.sh: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "gitea-credentials-template") {
		t.Error("B80 regression: generate-secrets.sh must create gitea-credentials-template Secret in argocd ns")
	}
	if !strings.Contains(s, "argocd.argoproj.io/secret-type: repo-creds") {
		t.Error("B80 regression: gitea-credentials-template Secret must carry repo-creds label so ArgoCD picks it up as a repository credential template")
	}
}

func TestB81ScopedClusterRoleCoversFrameworkCRDs(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/scripts/lib/scoped-kubeconfig.sh")
	if err != nil {
		t.Fatalf("read scoped-kubeconfig.sh: %v", err)
	}
	s := string(data)
	for _, group := range []string{
		"traefik.io",
		"cert-manager.io",
		"keda.sh",
		"external-secrets.io",
	} {
		if !strings.Contains(s, group) {
			t.Errorf("B81 regression: ClusterRole must grant access to apiGroup %q", group)
		}
	}
	// Namespace lifecycle is mandatory because ArgoCD uses CreateNamespace=true
	// when the destination namespace does not yet exist.
	if !strings.Contains(s, `resources: ["namespaces"]`) {
		t.Error("B81 regression: ClusterRole must include namespaces (CreateNamespace=true requires it)")
	}
}

func TestB82StaticAssetsChartExists(t *testing.T) {
	for _, f := range []string{
		"../../helm/static-assets/Chart.yaml",
		"../../helm/static-assets/values.yaml",
		"../../helm/static-assets/templates/deployment.yaml",
	} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("B82 regression: %s missing — required for static-frontend delivery", f)
		}
	}
	// The deployment template MUST mount an emptyDir at /etc/nginx/conf.d so
	// the upstream image's docker-entrypoint envsubst can render the gateway
	// config (readOnlyRootFilesystem otherwise silently keeps the stock
	// nginx default.conf and every request returns 404).
	dep, err := os.ReadFile("../../helm/static-assets/templates/deployment.yaml")
	if err != nil {
		t.Fatalf("read static-assets deployment.yaml: %v", err)
	}
	dstr := string(dep)
	if !strings.Contains(dstr, "/etc/nginx/conf.d") {
		t.Error("B82 regression: static-assets deployment must mount writable /etc/nginx/conf.d for envsubst")
	}
	if !strings.Contains(dstr, "name: nginx-conf-d") {
		t.Error("B82 regression: static-assets deployment must declare nginx-conf-d emptyDir volume")
	}
	rt, err := os.ReadFile("../../helmfile/cell.helmfile.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read cell.helmfile: %v", err)
	}
	rstr := string(rt)
	if !strings.Contains(rstr, "name: static-assets") {
		t.Error("B82 regression: cell.helmfile must include static-assets release")
	}
	if !strings.Contains(rstr, "../helm/static-assets") {
		t.Error("B82 regression: cell.helmfile must reference ../helm/static-assets chart")
	}
	// Workload cluster needs an `allow-static-assets` NetworkPolicy so Traefik
	// can reach the gateway through default-deny-ingress.
	np, err := os.ReadFile("../../helm/network-policies/templates/loopzero-system.yaml")
	if err != nil {
		t.Fatalf("read network-policies loopzero-system.yaml: %v", err)
	}
	npstr := string(np)
	if !strings.Contains(npstr, "name: allow-static-assets") {
		t.Error("B82 regression: NetworkPolicy allow-static-assets missing — Traefik traffic to gateway will be silently dropped")
	}
	if !strings.Contains(npstr, "kubernetes.io/metadata.name: traefik") {
		t.Error("B82 regression: allow-static-assets must whitelist traefik namespace as ingress source")
	}
}

func TestB82ScopedKubeconfigK3dRewrite(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/scripts/lib/scoped-kubeconfig.sh")
	if err != nil {
		t.Fatalf("read scoped-kubeconfig.sh: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `0\.0\.0\.0`) {
		t.Error("B82 regression: scoped-kubeconfig.sh must detect 0.0.0.0/127.0.0.1 server URLs (k3d local) and rewrite")
	}
	if !strings.Contains(s, `k3d-loopzero-${CLUSTER_ID}-server-0`) {
		t.Error("B82 regression: scoped-kubeconfig.sh must derive the docker-bridge IP from the k3d server-0 container")
	}
	if !strings.Contains(s, "docker inspect") {
		t.Error("B82 regression: scoped-kubeconfig.sh must docker inspect to resolve the bridge IP")
	}
}
