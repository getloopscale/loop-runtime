// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/argocd"
)

// fakeArgoListDelete is a minimal stub for the two new Provisioner methods
// used by pruneOrphanPreviewApps. Records all delete calls so the test can
// assert on the set of pruned Apps.
type fakeArgoListDelete struct {
	existing  []string
	deleted   []string
	listErr   error
	deleteErr error
}

func (f *fakeArgoListDelete) EnsureAppProject(_ context.Context, _ argocd.AppProjectSpec) error {
	return nil
}
func (f *fakeArgoListDelete) EnsureApplication(_ context.Context, _ argocd.ApplicationSpec) error {
	return nil
}
func (f *fakeArgoListDelete) DeleteApplications(_ context.Context, _ string) error { return nil }
func (f *fakeArgoListDelete) DeleteAppProject(_ context.Context, _ string) error   { return nil }
func (f *fakeArgoListDelete) ListApplicationNames(_ context.Context, _ string) ([]string, error) {
	return f.existing, f.listErr
}
func (f *fakeArgoListDelete) DeleteApplication(_ context.Context, appName string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, appName)
	return nil
}

// TestCanonicalDestNamespace_J23_69 pins the contract for the helper that
// resolves ArgoCD App.destination.namespace in tenant_gitops.go.
//
// J-23 LIVE catch 2026-06-07 #69 (sponsor verbatim "Atacar #69 fix
// tenant_gitops + CRD"): the renderer canonical refactor (commit
// cd6235fc) emits manifests with the three-segment namespace
// `loopzero-<tenant>-<project>-<env>`. Before this helper, tenant_gitops.go
// emitted destinations via the legacy two-segment formula
// `loopzero-<tenant>-<env>`, causing ArgoCD destination mismatch and
// OutOfSync forever on every production deploy.
//
// T2.1 of plan 2026-06-09-m2-operator-canonical-fix v1.4: empty
// projectSlug is now a CONTRACT VIOLATION rather than a silent fallback
// to the legacy two-segment formula. T1.1 ships CRD-level enforcement
// (TenantEnvironmentEntry.Project required + RFC 1123 Pattern) so every
// production caller (tenant_gitops.go:143, :186 + tenant_controller.go:337)
// passes a non-empty project. The two "fallback legacy" cases that
// previously asserted `loopzero-acme-{env}` were flipped to expect error —
// the silent fallback was the smoking gun behind all 4 M2 LIVE workarounds
// documented in feedback_m2_canonical_drift_operator_renderer.
func TestCanonicalDestNamespace_J23_69(t *testing.T) {
	cases := []struct {
		name    string
		tenant  string
		project string
		env     string
		want    string
		wantErr bool
	}{
		{
			name:    "canonical: tenant + project + env",
			tenant:  "tn-08fa2ecedc3c7e77",
			project: "teste",
			env:     "production",
			want:    "loopzero-tn-08fa2ecedc3c7e77-teste-production",
		},
		{
			name:    "canonical: preview env name (preview-pr-N)",
			tenant:  "acme",
			project: "webapp",
			env:     "preview-pr-42",
			want:    "loopzero-acme-webapp-preview-pr-42",
		},
		{
			// T2.1 RED: empty project must error (was M1 fallback pre-T2.1).
			name:    "error: empty project rejected — Project is required (T2.1)",
			tenant:  "acme",
			project: "",
			env:     "production",
			wantErr: true,
		},
		{
			// T2.1 RED: empty project must error even on preview-shaped env names.
			name:    "error: empty project rejected even on preview env (T2.1)",
			tenant:  "acme",
			project: "",
			env:     "staging",
			wantErr: true,
		},
		{
			name:    "error: empty tenant always invalid",
			tenant:  "",
			project: "webapp",
			env:     "production",
			wantErr: true,
		},
		{
			name:    "error: empty env always invalid",
			tenant:  "acme",
			project: "webapp",
			env:     "",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := canonicalDestNamespace(tc.tenant, tc.project, tc.env)
			if tc.wantErr {
				if err == nil {
					t.Errorf("canonicalDestNamespace(%q,%q,%q) = %q, want error",
						tc.tenant, tc.project, tc.env, got)
				}
				return
			}
			if err != nil {
				t.Errorf("canonicalDestNamespace(%q,%q,%q) unexpected error: %v",
					tc.tenant, tc.project, tc.env, err)
				return
			}
			if got != tc.want {
				t.Errorf("canonicalDestNamespace(%q,%q,%q) = %q, want %q",
					tc.tenant, tc.project, tc.env, got, tc.want)
			}
		})
	}
}

// TestCanonicalDestNamespaceRejectsEmptyProject — T2.1 of plan
// 2026-06-09-m2-operator-canonical-fix v1.4 explicit RED test
// per plan TDD body (lines 393-398). Asserts:
//
//   - Empty projectSlug returns error (no silent fallback to M1).
//   - Error message identifies the contract violation by name
//     ("Project is required"), which makes the error grep-friendly
//     in operator logs.
//
// Sibling assertion to the "fallback legacy" cases in
// TestCanonicalDestNamespace_J23_69 above; kept as a separate test
// function so the plan-cited symbol name is discoverable via
// `go test -run TestCanonicalDestNamespaceRejectsEmptyProject`.
func TestCanonicalDestNamespaceRejectsEmptyProject(t *testing.T) {
	got, err := canonicalDestNamespace("tn-test", "", "production")
	if err == nil {
		t.Fatalf("canonicalDestNamespace(tn-test, \"\", production) = %q, want error", got)
	}
	if !strings.Contains(err.Error(), "Project is required") {
		t.Errorf("error message %q must contain \"Project is required\"", err.Error())
	}
}

// TestBuildAppProjectDestNamespaces — T2.2 of plan
// 2026-06-09-m2-operator-canonical-fix v1.4. Asserts the (project, env)
// iteration in reconcileArgoCDApp emits one canonical M2 namespace per
// TenantEnvironmentEntry plus one per active PreviewEnvironment, in
// fail-loud mode (any canonicalDestNamespace error aborts the build).
//
// The plan TDD body declares the contract:
//
//	Given a Tenant with 2 projects each having a production env
//	when TenantController.reconcileArgoCDApp runs
//	then the AppProject spec.destinations contains both M2 namespaces
//	     (loopzero-tn-X-A-production AND loopzero-tn-X-B-production), never the
//	     M1 2-seg form.
func TestBuildAppProjectDestNamespaces(t *testing.T) {
	cases := []struct {
		name     string
		envs     []loopzerov1.TenantEnvironmentEntry
		previews []loopzerov1.PreviewEnvironment
		want     []string
		wantErr  string
	}{
		{
			name: "single project + production — one canonical M2 destination",
			envs: []loopzerov1.TenantEnvironmentEntry{
				{Name: "production", Cluster: "workload", Project: "myproject"},
			},
			want: []string{"loopzero-tn-test-myproject-production"},
		},
		{
			name: "two projects each with production — Cartesian emits 2 M2 destinations (LIVE T6.1 workaround codified)",
			envs: []loopzerov1.TenantEnvironmentEntry{
				{Name: "production", Cluster: "workload", Project: "proj-a"},
				{Name: "production", Cluster: "workload", Project: "proj-b"},
			},
			want: []string{
				"loopzero-tn-test-proj-a-production",
				"loopzero-tn-test-proj-b-production",
			},
		},
		{
			name: "one project + 3 envs — 3 M2 destinations",
			envs: []loopzerov1.TenantEnvironmentEntry{
				{Name: "production", Cluster: "workload", Project: "myapp"},
				{Name: "staging", Cluster: "workload", Project: "myapp"},
				{Name: "dev", Cluster: "workload", Project: "myapp"},
			},
			want: []string{
				"loopzero-tn-test-myapp-production",
				"loopzero-tn-test-myapp-staging",
				"loopzero-tn-test-myapp-dev",
			},
		},
		{
			name: "environments + preview — both contribute destinations in canonical M2",
			envs: []loopzerov1.TenantEnvironmentEntry{
				{Name: "production", Cluster: "workload", Project: "myapp"},
			},
			previews: []loopzerov1.PreviewEnvironment{
				{Spec: loopzerov1.PreviewEnvironmentSpec{ProjectID: "myapp", PRNumber: 7}},
			},
			want: []string{
				"loopzero-tn-test-myapp-production",
				"loopzero-tn-test-myapp-preview-pr-7",
			},
		},
		{
			name: "empty Project on env — fail-loud per T2.1 canonicalDestNamespace contract",
			envs: []loopzerov1.TenantEnvironmentEntry{
				{Name: "production", Cluster: "workload", Project: ""},
			},
			wantErr: "Project is required",
		},
		{
			name: "empty ProjectID on preview — fail-loud per EnvRef contract (ADR D2)",
			envs: nil,
			previews: []loopzerov1.PreviewEnvironment{
				{Spec: loopzerov1.PreviewEnvironmentSpec{ProjectID: "", PRNumber: 9}},
			},
			// Preview namespace derivation migrated to pkg/domain.NewEnvRef (ADR D2);
			// the EnvRef constructor rejects an empty project with "project empty".
			// (Was "project slug must not be empty" — the stale pre-migration
			// pkg/naming wording; the fail-loud invariant itself is unchanged.)
			wantErr: "project empty",
		},
		{
			name: "no envs no previews — empty destination list, no error",
			envs: nil,
			want: []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tenant := &loopzerov1.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: "tn-test"},
				Spec:       loopzerov1.TenantSpec{Environments: tc.envs},
			}
			got, err := buildAppProjectDestNamespaces(tenant, tc.previews)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil with destinations=%v", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error %q must contain %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d destinations %v, want %d %v", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("destinations[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestPruneOrphanPreviewApps pins the #71 contract:
//
//   - Apps whose name matches a spec.environments entry stay.
//   - Apps whose name matches an active PreviewEnvironment.PRNumber stay.
//   - Every other tenant-labeled App is pruned.
//
// LIVE catch 2026-06-07 regression test Fase 5: ArgoCD Apps for closed PRs
// stayed OutOfSync/Missing forever because tenant_gitops.reconcileArgoCDApp
// had emit paths for active envs + active previews but no prune pass for
// the orphans left behind after PR close. This test locks the new behaviour.
func TestPruneOrphanPreviewApps_J23_71(t *testing.T) {
	cases := []struct {
		name          string
		tenant        string
		envs          []string // spec.environments[*].Name
		activePreview []int    // PRNumber of active previews
		existing      []string // names currently in ArgoCD
		wantDeleted   []string
	}{
		{
			name:          "nothing to prune — all Apps match expected set",
			tenant:        "tn-acme",
			envs:          []string{"production"},
			activePreview: []int{1, 2},
			existing: []string{
				"tn-acme-default-project-production",
				"tn-acme-default-project-preview-pr-1",
				"tn-acme-default-project-preview-pr-2",
			},
			wantDeleted: []string{},
		},
		{
			name:          "prune App for closed PR",
			tenant:        "tn-acme",
			envs:          []string{"production"},
			activePreview: []int{1},
			existing: []string{
				"tn-acme-default-project-production",
				"tn-acme-default-project-preview-pr-1",
				"tn-acme-default-project-preview-pr-2", // PR 2 closed -> orphan
			},
			wantDeleted: []string{"tn-acme-default-project-preview-pr-2"},
		},
		{
			name:          "prune multiple orphans, keep production + active preview",
			tenant:        "tn-acme",
			envs:          []string{"production"},
			activePreview: []int{5},
			existing: []string{
				"tn-acme-default-project-production",
				"tn-acme-default-project-preview-pr-3",
				"tn-acme-default-project-preview-pr-4",
				"tn-acme-default-project-preview-pr-5",
				"tn-acme-default-project-staging-orphan",
			},
			wantDeleted: []string{
				"tn-acme-default-project-preview-pr-3",
				"tn-acme-default-project-preview-pr-4",
				"tn-acme-default-project-staging-orphan",
			},
		},
		{
			// #271: depois do rename, um App de nome LEGADO cai fora do conjunto
			// esperado e é deletado — COM o finalizer de cascade, ou seja, levando o
			// workload junto. É por isso que a limpeza do legado é PRÉ-deploy e não
			// pós: docs/operations/argocd-app-identity-migration-runbook.md
			name:          "App de nome legado é podado pelo prune canônico (por isso a limpeza é pré-deploy)",
			tenant:        "tn-acme",
			envs:          []string{"production"},
			activePreview: []int{},
			existing: []string{
				"tn-acme-default-project-production",
				"tn-acme-production",
			},
			wantDeleted: []string{"tn-acme-production"},
		},
		{
			name:          "empty existing — no work, no error",
			tenant:        "tn-acme",
			envs:          []string{"production"},
			activePreview: []int{},
			existing:      []string{},
			wantDeleted:   []string{},
		},
		{
			name:          "active envs only, no previews — production stays",
			tenant:        "tn-acme",
			envs:          []string{"production", "staging"},
			activePreview: []int{},
			existing: []string{
				"tn-acme-default-project-production",
				"tn-acme-default-project-staging",
				"tn-acme-default-project-preview-pr-99",
			},
			wantDeleted: []string{"tn-acme-default-project-preview-pr-99"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeArgoListDelete{existing: tc.existing}
			r := &TenantReconciler{ArgoCDProvisioner: fake}
			tenant := &loopzerov1.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: tc.tenant},
				Spec:       loopzerov1.TenantSpec{},
			}
			for _, env := range tc.envs {
				tenant.Spec.Environments = append(tenant.Spec.Environments,
					loopzerov1.TenantEnvironmentEntry{Name: env, Cluster: "workload", Project: "default-project"})
			}
			previews := make([]loopzerov1.PreviewEnvironment, 0, len(tc.activePreview))
			for _, pr := range tc.activePreview {
				previews = append(previews, loopzerov1.PreviewEnvironment{
					Spec: loopzerov1.PreviewEnvironmentSpec{PRNumber: pr, ProjectID: "default-project"},
				})
			}
			err := r.pruneOrphanPreviewApps(context.Background(), tenant, previews, "http://gitea/x.git")
			if err != nil {
				t.Fatalf("pruneOrphanPreviewApps unexpected error: %v", err)
			}
			sort.Strings(fake.deleted)
			sort.Strings(tc.wantDeleted)
			if len(fake.deleted) != len(tc.wantDeleted) {
				t.Errorf("deleted set size = %d, want %d (got=%v, want=%v)",
					len(fake.deleted), len(tc.wantDeleted), fake.deleted, tc.wantDeleted)
				return
			}
			for i := range fake.deleted {
				if fake.deleted[i] != tc.wantDeleted[i] {
					t.Errorf("deleted[%d] = %q, want %q (got=%v, want=%v)",
						i, fake.deleted[i], tc.wantDeleted[i], fake.deleted, tc.wantDeleted)
				}
			}
		})
	}
}

// TestPruneOrphanPreviewApps_ListErrorBubbles ensures a List failure
// surfaces as an error (caller logs + soft-fails) rather than silently
// proceeding with an empty existing set (which would prune nothing AND
// not log).
func TestPruneOrphanPreviewApps_ListErrorBubbles(t *testing.T) {
	fake := &fakeArgoListDelete{listErr: errors.New("argocd unreachable")}
	r := &TenantReconciler{ArgoCDProvisioner: fake}
	tenant := &loopzerov1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tn-acme"},
	}
	err := r.pruneOrphanPreviewApps(context.Background(), tenant, nil, "http://gitea/x.git")
	if err == nil {
		t.Fatal("expected error from List failure, got nil")
	}
	if len(fake.deleted) != 0 {
		t.Errorf("no deletes expected on list error, got %v", fake.deleted)
	}
}

// --- #271 D3: identidade canônica do Application (tenant, project, environment) ------
//
// Medido em app-dev 2026-08-08: appName e path omitiam env.Project enquanto destNS o
// incluía, então dois projetos do mesmo tenant no mesmo ambiente colidiam num único
// Application com destinos conflitantes (1519 escritas em 5 min). Plano:
// knowledge-base/plans/argocd-app-identity-canonical-plan.md

func tenantWithEnvs(envs ...loopzerov1.TenantEnvironmentEntry) *loopzerov1.Tenant {
	return &loopzerov1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tn-test"},
		Spec: loopzerov1.TenantSpec{
			CellID:       "cell-default",
			Environments: envs,
			GitOps:       loopzerov1.GitOpsSpec{Branch: "main"},
		},
	}
}

// TestBuildApplicationSpecs_ProjetosDistintosNaoColidem reproduz a colisão medida: dois
// projetos com o MESMO env.Name têm de produzir dois Applications, com paths distintos.
func TestBuildApplicationSpecs_ProjetosDistintosNaoColidem(t *testing.T) {
	r := &TenantReconciler{WorkloadClusterURL: "https://workload:6443"}
	tenant := tenantWithEnvs(
		loopzerov1.TenantEnvironmentEntry{Name: "preview", Cluster: "workload", Project: "alpha"},
		loopzerov1.TenantEnvironmentEntry{Name: "preview", Cluster: "workload", Project: "beta"},
	)

	specs, err := r.buildApplicationSpecs(tenant, nil, "http://gitea/tn-test-gitops.git")
	if err != nil {
		t.Fatalf("buildApplicationSpecs: %v", err)
	}

	nomes := map[string]bool{}
	paths := map[string]bool{}
	for _, s := range specs {
		nomes[s.Name] = true
		paths[s.Path] = true
	}
	if len(nomes) != 2 {
		t.Errorf("dois projetos no mesmo ambiente devem produzir 2 Applications distintos; "+
			"veio %d nome(s) %v — é a colisão da #271", len(nomes), nomes)
	}
	if len(paths) != 2 {
		t.Errorf("cada Application deve ver só a subárvore do seu projeto; veio %d path(s) %v "+
			"(gitea_pusher.go:136 escreve em {env}/{project}/)", len(paths), paths)
	}
	if !nomes["tn-test-alpha-preview"] || !nomes["tn-test-beta-preview"] {
		t.Errorf("formato canônico é {tenant}-{project}-{env} (pkg/naming.ArgoAppName); veio %v", nomes)
	}
}

// TestBuildApplicationSpecs_ProjectVazioFalhaAlto — mesma disciplina que
// canonicalDestNamespace já tem (tenant_gitops.go:49-51): Project vazio não pode
// gerar um nome degenerado em silêncio.
func TestBuildApplicationSpecs_ProjectVazioFalhaAlto(t *testing.T) {
	r := &TenantReconciler{WorkloadClusterURL: "https://workload:6443"}
	tenant := tenantWithEnvs(
		loopzerov1.TenantEnvironmentEntry{Name: "production", Cluster: "workload", Project: ""},
	)

	if _, err := r.buildApplicationSpecs(tenant, nil, "http://gitea/x.git"); err == nil {
		t.Fatal("Project vazio deve propagar erro, não gerar Application com nome degenerado")
	}
}

// TestPruneExpectedIgualAoQueEnsureCria é o teste que torna impossível o cenário
// destrutivo: pruneOrphanPreviewApps deleta toda Application do tenant fora do conjunto
// `expected`, e o finalizer de cascade faz esse delete apagar o workload. Se `expected`
// for computado por uma fórmula diferente da que cria os Apps, o operator deleta em
// cascata exatamente o que acabou de criar. As duas leituras têm de vir da MESMA função.
func TestPruneExpectedIgualAoQueEnsureCria(t *testing.T) {
	r := &TenantReconciler{WorkloadClusterURL: "https://workload:6443"}
	tenant := tenantWithEnvs(
		loopzerov1.TenantEnvironmentEntry{Name: "production", Cluster: "workload", Project: "alpha"},
		loopzerov1.TenantEnvironmentEntry{Name: "preview", Cluster: "workload", Project: "beta"},
	)

	specs, err := r.buildApplicationSpecs(tenant, nil, "http://gitea/tn-test-gitops.git")
	if err != nil {
		t.Fatalf("buildApplicationSpecs: %v", err)
	}
	criados := map[string]bool{}
	for _, s := range specs {
		criados[s.Name] = true
	}

	esperados, err := r.expectedApplicationNames(tenant, nil, "http://gitea/tn-test-gitops.git")
	if err != nil {
		t.Fatalf("expectedApplicationNames: %v", err)
	}

	for name := range criados {
		if !esperados[name] {
			t.Errorf("o prune deletaria %q, que o próprio reconcile acabou de criar — "+
				"com o finalizer de cascade isso apaga o workload do tenant", name)
		}
	}
	for name := range esperados {
		if !criados[name] {
			t.Errorf("o prune espera %q, que ninguém cria — nome órfão no conjunto de proteção", name)
		}
	}
}
