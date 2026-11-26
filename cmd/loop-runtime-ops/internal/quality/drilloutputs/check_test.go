// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package drilloutputs

import (
	"os"
	"path/filepath"
	"testing"
)

// repo builds a temp scan root with the given files, plus an empty
// infra/pulumi-drill/ dir (the excluded source location).
func repo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "infra", "pulumi-drill", "src"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

func pass(t *testing.T, root string) bool {
	t.Helper()
	o, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return o.Pass
}

func TestCleanRepoPasses(t *testing.T) {
	root := repo(t, map[string]string{
		"infra/pulumi-drill/src/index.ts": "export const outputs = {};\n",
	})
	if !pass(t, root) {
		t.Error("clean repo should pass")
	}
}

func TestCatchesLegacyPulumiStackOutputCluster(t *testing.T) {
	root := repo(t, map[string]string{
		".github/workflows/fake.yml": "      - run: pulumi stack output cluster\n",
	})
	if pass(t, root) {
		t.Error("should catch bare `pulumi stack output cluster`")
	}
}

func TestCatchesLegacyClusterKubeconfig(t *testing.T) {
	root := repo(t, map[string]string{
		"scripts/fake.sh": "KUBECONFIG=$(pulumi stack output clusterKubeconfig)\n",
	})
	if pass(t, root) {
		t.Error("should catch clusterKubeconfig")
	}
}

func TestAcceptsNewSchemaClusterNames(t *testing.T) {
	root := repo(t, map[string]string{
		".github/workflows/good.yml": "      - run: |\n" +
			"          MGMT=$(pulumi stack output mgmtCluster)\n" +
			"          BUILD=$(pulumi stack output buildCluster)\n" +
			"          RUNTIME=$(pulumi stack output runtimeCluster)\n" +
			"          RUNTIME2=$(pulumi stack output runtimeCluster2)\n",
	})
	if !pass(t, root) {
		t.Error("v1-era plain cluster names (mgmtCluster etc.) should not match the v0 singular pattern")
	}
}

func TestCatchesV1NestedMgmtClusterKubeconfig(t *testing.T) {
	root := repo(t, map[string]string{
		".github/workflows/fake-v1.yml": "      - run: pulumi stack output --json | jq -r '.outputs.mgmtCluster.kubeconfig'\n",
	})
	if pass(t, root) {
		t.Error("should catch v1 nested .outputs.mgmtCluster.kubeconfig")
	}
}

func TestCatchesV1NestedPgHost(t *testing.T) {
	root := repo(t, map[string]string{
		"scripts/fake-pg.sh": "PG_HOST=$(pulumi stack output --json | jq -r '.outputs.pg.host')\n",
	})
	if pass(t, root) {
		t.Error("should catch v1 .outputs.pg.host")
	}
}

func TestAcceptsV2NamedExports(t *testing.T) {
	root := repo(t, map[string]string{
		".github/workflows/good-v2.yml": "      - run: |\n" +
			"          KC=$(pulumi stack output --stack drill mgmtClusterKubeconfig --show-secrets)\n" +
			"          PG=$(pulumi stack output --stack drill pgHost)\n" +
			"          BUCKET=$(pulumi stack output --stack drill bucketBuilds)\n",
	})
	if !pass(t, root) {
		t.Error("v2 flat named exports should pass")
	}
}

func TestCatchesMultilineJqClusterConcat(t *testing.T) {
	root := repo(t, map[string]string{
		"scripts/fake-jq.yml": "        npx pulumi stack output --json | jq -r '\n" +
			"          .outputs |\n" +
			"          \"  mgmt: \" + .mgmtCluster.endpoint,\n" +
			"          \"  build: \" + .buildCluster.endpoint\n        '\n",
	})
	if pass(t, root) {
		t.Error("should catch jq concat `\" + .mgmtCluster.endpoint`")
	}
}

func TestCatchesMultilineJqPgConcat(t *testing.T) {
	root := repo(t, map[string]string{
		"scripts/fake-pg-jq.sh": "PG=$(npx pulumi stack output --json | jq -r '\"  pg: \" + .pg.host')\n",
	})
	if pass(t, root) {
		t.Error("should catch jq concat `\" + .pg.host`")
	}
}

func TestCatchesPhantomApiUrl(t *testing.T) {
	root := repo(t, map[string]string{
		".github/workflows/phantom.yml": "      - run: API_URL=$(npx pulumi stack output apiUrl --stack drill)\n",
	})
	if pass(t, root) {
		t.Error("should catch phantom `pulumi stack output apiUrl`")
	}
}

func TestExcludesPulumiDrillSource(t *testing.T) {
	// A legacy pattern INSIDE infra/pulumi-drill is allowed (source location).
	root := repo(t, map[string]string{
		"infra/pulumi-drill/src/legacy.ts": "const x = data.outputs.pg.host;\n",
	})
	if !pass(t, root) {
		t.Error("patterns inside infra/pulumi-drill source should be excluded")
	}
}

func TestExcludesKnowledgeBaseDocs(t *testing.T) {
	root := repo(t, map[string]string{
		".claude/knowledge-base/concepts/operations/note.md": "legacy: pulumi stack output cluster\n",
	})
	if !pass(t, root) {
		t.Error(".claude docs quoting a pattern should be excluded")
	}
}

func TestExcludesNestedNodeModules(t *testing.T) {
	// A pattern inside a nested node_modules/ dir is excluded by component name.
	root := repo(t, map[string]string{
		"cli/node_modules/pkg/x.js": "pulumi stack output cluster\n",
	})
	if !pass(t, root) {
		t.Error("nested node_modules should be excluded by component name")
	}
}

func TestNonScannedExtensionIgnored(t *testing.T) {
	root := repo(t, map[string]string{
		"README.txt": "pulumi stack output cluster\n", // .txt not scanned
	})
	if !pass(t, root) {
		t.Error(".txt files should not be scanned")
	}
}

func TestFindingFormatIncludesFileAndLine(t *testing.T) {
	root := repo(t, map[string]string{
		"scripts/a.sh": "line1\nDRILL_CLUSTER_KUBECONFIG=x\n",
	})
	o, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass || len(o.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %v", o.Findings)
	}
	if want := "scripts/a.sh:2:"; !contains(o.Findings[0], want) {
		t.Errorf("finding %q should contain %q", o.Findings[0], want)
	}
}

func TestNonexistentRootErrors(t *testing.T) {
	// WalkDir invokes the walk fn with a non-nil error for a missing root;
	// Check propagates it (exercises the walkErr + err return paths).
	if _, err := Check(Config{Root: "/nonexistent/drill/root"}); err == nil {
		t.Error("expected error for nonexistent root")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
