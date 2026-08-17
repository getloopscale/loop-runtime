// Hermetic tests for the openbaoinitauth gate.
// Migrated from bash test infra/tests/check-openbao-init-auth-mounts_test.sh
// (M-Migrate2 2026-07-10 M16 T3.7 Wave 2).
package openbaoinitauth

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCheck_ChartMissing_FAIL(t *testing.T) {
	root := t.TempDir()
	_, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrChartMissing) {
		t.Fatalf("expected ErrChartMissing, got %v", err)
	}
}

func TestCheck_HelmAbsent_WARN(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, chartPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	oldPath := os.Getenv("PATH")
	defer os.Setenv("PATH", oldPath)
	os.Setenv("PATH", "/nonexistent")
	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("expected nil (WARN), got %v", err)
	}
	if !outcome.Warn {
		t.Fatalf("expected Warn=true")
	}
}

func TestCheck_RealRepoWithHelm(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not on PATH — skipping")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := cwd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(root, "ROADMAP.md")); err == nil {
			break
		}
		root = filepath.Dir(root)
	}
	if _, err := os.Stat(filepath.Join(root, "ROADMAP.md")); err != nil {
		t.Skip("could not locate ROADMAP.md")
	}
	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("real-repo violation: %v; %v", err, outcome.Failures)
	}
}

// No shell script writes a Vault policy.
//
// A policy says what an identity MAY do. That is declarative, reviewable and
// diffable, so it belongs in the chart — and the chart already declares it. What
// belongs in a script is the credential LIFECYCLE: creating the AppRole,
// generating the secret_id, materialising the Secret. Those only run against a
// live vault and cannot be templated.
//
// Measured 2026-08-13 (B-099): infra/scripts/local/vault-bootstrap-dev.sh ran
// `bao policy write loopzero-api` minutes AFTER helmfile applied the chart's policy
// of the same name. Last writer wins, so the versioned one never reached any
// runtime — and slice-17's fix to it (B-072) was invisible. Worse, the script's
// version granted `delete` on transit/keys/*, the crypto-shredding verb, to an
// identity that per B-075 has no shredding path at all.
//
// Decision: MEET-ASYNC-AMENDMENT-2026-08-13-vault-policy-ownership.
func TestOpenBaoInitAuth_NoScriptWritesAPolicyTheChartOwns(t *testing.T) {
	root, err := filepath.Abs("../../../../..")
	if err != nil {
		t.Fatal(err)
	}

	// Three forms, not one. The review wrote a script with each and the
	// single-string version passed all three: the OpenBao docs show
	// `sys/policies/acl/<name>` as the API path, a verb held in a variable
	// defeats a literal match, and curl to the same path defeats both.
	forms := []*regexp.Regexp{
		regexp.MustCompile(`policy\s+write`),
		regexp.MustCompile(`sys/policies/acl`),
	}

	var offenders []string
	scanned := 0
	// Every shell in the repo, not just infra/. Nothing outside it writes a
	// policy today — measured — so this is coverage, not a live defect; but the
	// next script does not have to land in infra/ to cause the same collision.
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable path is not a policy write
		}
		if info.IsDir() {
			if n := info.Name(); n == ".git" || n == "node_modules" || n == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".sh") {
			return nil
		}
		b, readErr := os.ReadFile(path) //nolint:gosec // walking a repo-relative tree
		if readErr != nil {
			return readErr
		}
		scanned++
		for i, line := range strings.Split(string(b), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
			for _, f := range forms {
				if f.MatchString(line) {
					rel, _ := filepath.Rel(root, path)
					offenders = append(offenders, fmt.Sprintf("%s:%d: %s", rel, i+1, trimmed))
					break
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking infra/: %v", walkErr)
	}
	// A walk that read nothing cannot vouch for anything.
	if scanned == 0 {
		t.Fatal("no shell script scanned under infra/ — the assertion measured nothing")
	}
	if len(offenders) > 0 {
		t.Errorf("%d shell line(s) write a Vault policy. The chart is the sole declaration; a script "+
			"writing one of the same name silently wins, and the versioned version — the one that gets "+
			"reviewed — never reaches runtime:\n  %s\nMove the rules into infra/helm/openbao-init and "+
			"keep only the AppRole binding in the script.",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

// The chart authorises the key name the code actually asks for.
//
// The client builds `/v1/transit/keys/<tenantKeyPrefix><id>` and the policy
// grants a path prefix. If the two drift, every tenant's first encrypt is denied
// with a 403 that reads like a network fault — and nothing in the build catches
// it, because one lives in Go and the other in YAML.
//
// This is the fourth bullet of B-072, and the fifth time this session that the
// remedy is the same: compare the declaration against the source that executes,
// rather than trusting that two hand-maintained copies agree.
func TestOpenBaoInitAuth_ChartGrantsTheKeyPrefixTheCodeNames(t *testing.T) {
	root, err := filepath.Abs("../../../../..")
	if err != nil {
		t.Fatal(err)
	}

	// The executing source: the Go constant the client concatenates.
	goSrc, err := os.ReadFile(filepath.Join(root, "pkg", "vault", "transit_client.go"))
	if err != nil {
		t.Fatalf("reading the transit client: %v", err)
	}
	m := regexp.MustCompile(`tenantKeyPrefix\s*=\s*"([^"]+)"`).FindSubmatch(goSrc)
	if m == nil {
		t.Fatal("tenantKeyPrefix not found in pkg/vault/transit_client.go — either the client stopped " +
			"naming keys by prefix, or this assertion stopped being able to read it")
	}
	prefix := string(m[1])

	values, err := os.ReadFile(filepath.Join(root, "infra", "helm", "openbao-init", "values.yaml"))
	if err != nil {
		t.Fatalf("reading the chart values: %v", err)
	}
	for _, verb := range []string{"keys", "encrypt", "decrypt"} {
		want := `path "transit/` + verb + `/` + prefix + `*"`
		if !strings.Contains(string(values), want) {
			t.Errorf("the chart does not grant %s.\nThe client builds /v1/transit/%s/%s<id> "+
				"(pkg/vault/transit_client.go), so every tenant's first call on that path is denied — "+
				"and a 403 there reads like a transient fault.", want, verb, prefix)
		}
	}
}

// No policy grants delete on the key prefix the API actually creates.
//
// `delete` on transit/keys is the crypto-shredding verb: it makes every
// ciphertext encrypted under that key permanently unrecoverable. It is not a
// permission any identity should hold by accident, and until 2026-08-13 the dev
// bootstrap granted it to loopzero-api — which per B-075 has no shredding path at
// all: DeleteTenantKey is an interface with no implementation and no caller.
//
// The review pointed out that removing it from the script made it absent, and
// nothing made it STAY absent. Presence checks cannot express that; this is the
// absence check, and it is what defends the property rather than the edit.
//
// Scoped to the API's prefix on purpose. The chart DOES grant delete on
// `transit/keys/tenant-+` to tenant-controller-transit and loopzero-tenant-operator,
// and that is a deliberate, documented design — the doc-comment above it names
// the crypto-shredding pattern explicitly. Forbidding it there would be this
// check overruling a decision instead of guarding one.
//
// Worth recording, because the first version of this assertion found it: those
// grants are on `tenant-<id>`, and the API creates `loopzero-tenants-<id>`. The
// operator is authorised to delete a key name nobody ever creates — the same
// divergence B-075 measured from the code side, confirmed here from the policy
// side. Wiring that path today would delete nothing and report success.
//
// Reintroducing delete on the API prefix needs the B-075 decision on who owns
// shredding first — not a permission that drifted back in.
func TestOpenBaoInitAuth_NoPolicyGrantsDeleteOnATransitKey(t *testing.T) {
	root, err := filepath.Abs("../../../../..")
	if err != nil {
		t.Fatal(err)
	}
	values, err := os.ReadFile(filepath.Join(root, "infra", "helm", "openbao-init", "values.yaml"))
	if err != nil {
		t.Fatalf("reading the chart values: %v", err)
	}

	// A asserção cobre QUALQUER caminho `transit/keys/`, e não só o prefixo que o api
	// cria. A versão anterior era estreita porque a pergunta estava em aberto: ela
	// dizia, na própria mensagem, *"who owns shredding is B-075, still undecided; a
	// permission is not the place to answer it"* — e recusar o `delete` em `tenant-+`
	// naquele momento teria sido este teste ANULANDO uma decisão em vez de guardá-la.
	//
	// A pergunta foi respondida em 2026-08-16 (`B-075`, ratificado): o shredding é
	// **operator-side quando construído**, e enquanto não existir **nenhuma policy
	// concede `delete`** em chave transit. As duas concessões sobre `tenant-+` foram
	// removidas na mesma fatia. A partir daqui a proibição guarda a decisão, que é
	// exatamente o que a versão anterior dizia não poder fazer ainda.
	//
	// Quando o shredder for construído, a concessão volta — apontando para o prefixo
	// que o api de fato cria, de propósito, e esta asserção é emendada junto, na mesma
	// mudança que a torna necessária.
	lines := strings.Split(string(values), "\n")
	inTransitKeys := false
	var keyPath string
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if m := regexp.MustCompile(`path "(transit/keys/[^"]*)"`).FindStringSubmatch(line); m != nil {
			inTransitKeys, keyPath = true, m[1]
			continue
		}
		if inTransitKeys {
			if strings.Contains(line, "capabilities") {
				if strings.Contains(line, `"delete"`) {
					t.Errorf("infra/helm/openbao-init/values.yaml:%d grants delete on %q:\n  %s\n\n"+
						"That is the crypto-shredding verb — it makes every ciphertext under the key "+
						"permanently unrecoverable. B-075 (ratified 2026-08-16) decided that shredding "+
						"is operator-side WHEN BUILT, and that until it exists no policy carries the "+
						"verb: a grant nothing calls is destructive power with zero function.\n\n"+
						"If you are adding this back because you are building the shredder, amend this "+
						"assertion in the same change — and point the grant at the prefix the api "+
						"actually creates (pkg/vault/transit_client.go tenantKeyPrefix), not at "+
						"`tenant-`, which nothing has ever created.", i+1, keyPath, trimmed)
				}
				inTransitKeys = false
			}
			if strings.HasPrefix(trimmed, "}") {
				inTransitKeys = false
			}
		}
	}
}
