// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package helmdiffstructural

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writePair(t *testing.T, base, pr string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	bp := filepath.Join(dir, "base.yaml")
	pp := filepath.Join(dir, "pr.yaml")
	require.NoError(t, os.WriteFile(bp, []byte(base), 0o600))
	require.NoError(t, os.WriteFile(pp, []byte(pr), 0o600))
	return bp, pp
}

// =============================================================================
// kindLineRe / countKind
// =============================================================================

func TestCountKind_BasicMatch(t *testing.T) {
	body := `---
kind: NetworkPolicy
metadata:
  name: a
---
kind: NetworkPolicy
metadata:
  name: b
---
kind: ConfigMap
metadata:
  name: c
`
	assert.Equal(t, 2, countKind(body, "NetworkPolicy"))
	assert.Equal(t, 1, countKind(body, "ConfigMap"))
	assert.Equal(t, 0, countKind(body, "PodSecurityPolicy"))
}

func TestCountKind_ExcludesCommentMatches(t *testing.T) {
	body := `# kind: NetworkPolicy (legacy reference in a comment)
kind: ConfigMap
`
	assert.Equal(t, 0, countKind(body, "NetworkPolicy"),
		"comment line not anchored at start must not count")
}

// =============================================================================
// extractTuples
// =============================================================================

func TestExtractTuples_BasicLayout(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "m.yaml")
	body := `---
kind: NetworkPolicy
metadata:
  name: net-a
  namespace: prod
spec:
  podSelector: {}
---
kind: ConfigMap
metadata:
  name: cfg-b
  namespace: loopruntime-system
data:
  k: v
`
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	got, err := extractTuples(p)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "ConfigMap", got[0].Kind)
	assert.Equal(t, "cfg-b", got[0].Name)
	assert.Equal(t, "loopruntime-system", got[0].Namespace)
	assert.Equal(t, "NetworkPolicy", got[1].Kind)
	assert.Equal(t, "prod", got[1].Namespace)
}

func TestExtractTuples_QuotedValues(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "m.yaml")
	body := `kind: ConfigMap
metadata:
  name: "quoted-name"
  namespace: "quoted-ns"
`
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	got, err := extractTuples(p)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "quoted-name", got[0].Name)
	assert.Equal(t, "quoted-ns", got[0].Namespace)
}

func TestExtractTuples_MissingFile(t *testing.T) {
	_, err := extractTuples("/tmp/zzz-no-helm-diff-file.yaml")
	require.Error(t, err)
}

// =============================================================================
// findNamespaceMoves
// =============================================================================

func TestFindNamespaceMoves_DetectsChange(t *testing.T) {
	base := []Tuple{{Kind: "Service", Name: "api", Namespace: "loopruntime-system"}}
	pr := []Tuple{{Kind: "Service", Name: "api", Namespace: "kube-system"}}
	got := findNamespaceMoves(base, pr)
	require.Len(t, got, 1)
	assert.Equal(t, "STRUCT-2", got[0].Code)
	assert.Contains(t, got[0].Message, "loopruntime-system")
	assert.Contains(t, got[0].Message, "kube-system")
}

func TestFindNamespaceMoves_NoChange(t *testing.T) {
	base := []Tuple{{Kind: "Service", Name: "api", Namespace: "loopruntime-system"}}
	pr := []Tuple{{Kind: "Service", Name: "api", Namespace: "loopruntime-system"}}
	assert.Empty(t, findNamespaceMoves(base, pr))
}

func TestFindNamespaceMoves_NewResourceIgnored(t *testing.T) {
	base := []Tuple{}
	pr := []Tuple{{Kind: "Service", Name: "api", Namespace: "loopruntime-system"}}
	assert.Empty(t, findNamespaceMoves(base, pr), "additions are NOT STRUCT-2")
}

// =============================================================================
// findSecurityDowngrades
// =============================================================================

func TestFindSecurityDowngrades_RunAsNonRootRemoved(t *testing.T) {
	diff := `--- a
+++ b
@@ -1 +1 @@
-      runAsNonRoot: true
+      runAsNonRoot: false
`
	got := findSecurityDowngrades(diff)
	require.Len(t, got, 1)
	assert.Equal(t, "STRUCT-3a", got[0].Code)
}

func TestFindSecurityDowngrades_RunAsNonRootMovedUnchanged(t *testing.T) {
	diff := `--- a
+++ b
-      runAsNonRoot: true
+      runAsNonRoot: true
`
	assert.Empty(t, findSecurityDowngrades(diff),
		"both - and + present (move/no-op) must NOT trigger STRUCT-3a")
}

func TestFindSecurityDowngrades_ReadOnlyRootFilesystem(t *testing.T) {
	diff := `-      readOnlyRootFilesystem: true
+      readOnlyRootFilesystem: false
`
	got := findSecurityDowngrades(diff)
	require.Len(t, got, 1)
	assert.Equal(t, "STRUCT-3b", got[0].Code)
}

func TestFindSecurityDowngrades_AllowPrivilegeEscalation(t *testing.T) {
	diff := `+      allowPrivilegeEscalation: true
`
	got := findSecurityDowngrades(diff)
	require.Len(t, got, 1)
	assert.Equal(t, "STRUCT-3c", got[0].Code)
}

func TestFindSecurityDowngrades_Privileged(t *testing.T) {
	diff := `+      privileged: true
`
	got := findSecurityDowngrades(diff)
	require.Len(t, got, 1)
	assert.Equal(t, "STRUCT-3d", got[0].Code)
}

func TestFindSecurityDowngrades_NoFalsePositiveOnUnchanged(t *testing.T) {
	diff := `       runAsNonRoot: true
       readOnlyRootFilesystem: true
`
	assert.Empty(t, findSecurityDowngrades(diff),
		"context lines (no +/-) must NOT trigger")
}

// =============================================================================
// Check — end-to-end
// =============================================================================

func TestCheck_IdenticalFilesPass(t *testing.T) {
	body := "kind: ConfigMap\nmetadata:\n  name: a\n"
	bp, pp := writePair(t, body, body)
	out, err := Check(Config{Chart: "demo", BasePath: bp, PRPath: pp})
	require.NoError(t, err)
	assert.True(t, out.Pass)
	assert.True(t, out.Identical)
	assert.Contains(t, strings.Join(out.Messages, "\n"), "no diff")
}

func TestCheck_BenignDiffPasses(t *testing.T) {
	base := "kind: ConfigMap\nmetadata:\n  name: a\n  namespace: ns1\ndata:\n  k: v1\n"
	pr := "kind: ConfigMap\nmetadata:\n  name: a\n  namespace: ns1\ndata:\n  k: v2\n"
	bp, pp := writePair(t, base, pr)
	out, err := Check(Config{Chart: "demo", BasePath: bp, PRPath: pp})
	require.NoError(t, err)
	assert.True(t, out.Pass)
	assert.False(t, out.Identical)
	assert.Empty(t, out.Violations)
}

func TestCheck_NetworkPolicyRemovalBlocks(t *testing.T) {
	base := `---
kind: NetworkPolicy
metadata:
  name: deny-all
  namespace: prod
---
kind: NetworkPolicy
metadata:
  name: allow-ingress
  namespace: prod
`
	pr := `---
kind: NetworkPolicy
metadata:
  name: deny-all
  namespace: prod
`
	bp, pp := writePair(t, base, pr)
	out, err := Check(Config{Chart: "demo", BasePath: bp, PRPath: pp})
	require.NoError(t, err)
	assert.False(t, out.Pass)
	require.GreaterOrEqual(t, len(out.Violations), 1)
	codes := []string{}
	for _, v := range out.Violations {
		codes = append(codes, v.Code)
	}
	assert.Contains(t, codes, "STRUCT-1")
}

func TestCheck_NamespaceMoveBlocks(t *testing.T) {
	base := "kind: Service\nmetadata:\n  name: api\n  namespace: loopruntime-system\n"
	pr := "kind: Service\nmetadata:\n  name: api\n  namespace: kube-system\n"
	bp, pp := writePair(t, base, pr)
	out, err := Check(Config{Chart: "demo", BasePath: bp, PRPath: pp})
	require.NoError(t, err)
	assert.False(t, out.Pass)
	codes := []string{}
	for _, v := range out.Violations {
		codes = append(codes, v.Code)
	}
	assert.Contains(t, codes, "STRUCT-2")
}

func TestCheck_SecurityDowngradeBlocks(t *testing.T) {
	base := `kind: Deployment
metadata:
  name: app
spec:
  template:
    spec:
      containers:
        - name: c
          securityContext:
            runAsNonRoot: true
`
	pr := `kind: Deployment
metadata:
  name: app
spec:
  template:
    spec:
      containers:
        - name: c
          securityContext:
            runAsNonRoot: false
`
	bp, pp := writePair(t, base, pr)
	out, err := Check(Config{Chart: "demo", BasePath: bp, PRPath: pp})
	require.NoError(t, err)
	assert.False(t, out.Pass)
	codes := []string{}
	for _, v := range out.Violations {
		codes = append(codes, v.Code)
	}
	assert.Contains(t, codes, "STRUCT-3a")
}

func TestCheck_MissingChartName(t *testing.T) {
	bp, pp := writePair(t, "kind: x\n", "kind: x\n")
	_, err := Check(Config{BasePath: bp, PRPath: pp})
	require.Error(t, err)
	var te *loopruntimeerrors.LoopRuntimeError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, "OPS_USAGE", te.Code)
}

func TestCheck_MissingBaseFile(t *testing.T) {
	dir := t.TempDir()
	pp := filepath.Join(dir, "pr.yaml")
	require.NoError(t, os.WriteFile(pp, []byte("x"), 0o600))
	_, err := Check(Config{Chart: "demo", BasePath: "/tmp/zzz-no-base.yaml", PRPath: pp})
	require.Error(t, err)
}

func TestCheck_MissingPRFile(t *testing.T) {
	dir := t.TempDir()
	bp := filepath.Join(dir, "base.yaml")
	require.NoError(t, os.WriteFile(bp, []byte("x"), 0o600))
	_, err := Check(Config{Chart: "demo", BasePath: bp, PRPath: "/tmp/zzz-no-pr.yaml"})
	require.Error(t, err)
}

func TestCheck_BothMissingPaths(t *testing.T) {
	_, err := Check(Config{Chart: "demo"})
	require.Error(t, err)
	var te *loopruntimeerrors.LoopRuntimeError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, "OPS_USAGE", te.Code)
}

// =============================================================================
// runDiff (uses /usr/bin/diff)
// =============================================================================

func TestRunDiff_FilesDiffer(t *testing.T) {
	bp, pp := writePair(t, "a\n", "b\n")
	got, err := runDiff(bp, pp)
	require.NoError(t, err)
	assert.Contains(t, got, "-a")
	assert.Contains(t, got, "+b")
}

func TestRunDiff_Identical(t *testing.T) {
	bp, pp := writePair(t, "same\n", "same\n")
	got, err := runDiff(bp, pp)
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

// =============================================================================
// Constants
// =============================================================================

func TestSensitiveKinds(t *testing.T) {
	assert.Contains(t, SensitiveKinds, "NetworkPolicy")
	assert.Contains(t, SensitiveKinds, "ClusterRoleBinding")
	assert.Contains(t, SensitiveKinds, "PodSecurityPolicy")
}
