// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package secrets

import (
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

// seed-loopruntime-secrets.sh (T8.1 EC-21 of plan 2026-06-02-loopruntime-api-vault-integration)
// writes every value the ExternalSecret inventory names into OpenBao, at the path
// the inventory resolves for it. Two properties carry the weight: a key whose
// vaultBinaryOverride points at another binary is written under THAT binary, and a
// key with no value is skipped, not failed.

const seedScript = openbaoScripts + "/seed-loopruntime-secrets.sh"

// seedInventory mirrors the real schema: three ExternalSecrets, five keys, one of
// them overridden onto the api's path.
const seedInventory = `envName: ""
secretStoreRef:
  kind: ClusterSecretStore
  name: loopruntime-secrets-secrets
defaultRefreshInterval: 1h
externalSecrets:
  - targetSecret: gitea-admin
    targetNamespace: loopruntime-system
    vaultBinary: api
    vaultCategory: secrets
    data:
      - secretKey: token
        vaultKey: gitea/token

  - targetSecret: loopruntime-api-stripe
    targetNamespace: loopruntime-system
    vaultBinary: api
    vaultCategory: secrets
    data:
      - secretKey: STRIPE_SECRET_KEY
        vaultKey: stripe/secret_key

  - targetSecret: loopruntime-tenant-operator
    targetNamespace: loopruntime-system
    vaultBinary: operators
    vaultCategory: secrets
    data:
      - secretKey: GITEA_TOKEN
        vaultKey: gitea/token
        vaultBinaryOverride: api
      - secretKey: DIGITALOCEAN_TOKEN
        vaultKey: digitalocean/token
      - secretKey: SPACES_ACCESS_KEY_ID
        vaultKey: spaces/access_key_id
`

// The fixture values are recognisable strings, not credential-shaped ones: the
// leak test looks for them in the output, and nothing reads them as credentials.
const (
	seedGiteaValue  = "fixture-gitea-value"
	seedStripeValue = "fixture-stripe-value"
	seedDOValue     = "fixture-digitalocean-value"
	seedSpacesValue = "fixture-spaces-value"
)

// seedValuesPartial leaves spaces/access_key_id out, to exercise the SKIP path.
const seedValuesPartial = `secrets:
  api:
    gitea/token: "` + seedGiteaValue + `"
    stripe/secret_key: "` + seedStripeValue + `"
  operators:
    digitalocean/token: "` + seedDOValue + `"
`

const seedValuesFull = seedValuesPartial + `    spaces/access_key_id: "` + seedSpacesValue + `"
`

var seedSession = []string{"BAO_ADDR=https://fixture:8200", "BAO_TOKEN=fixture"}

func init() {
	registerFakes("seed-loopruntime-secrets", map[string]fakeTool{
		"bao": func(args []string) int {
			recordCall("FAKE_BAO_LOG", "BAO_CALL", args)
			return envExit("FAKE_BAO_EXIT")
		},
	})
}

// seedFiles writes the inventory and both values files and returns their paths.
func seedFiles(t *testing.T) (inventory, partial, full string) {
	t.Helper()
	dir := t.TempDir()
	return writeFixture(t, dir, "inventory.yaml", seedInventory),
		writeFixture(t, dir, "values.yaml", seedValuesPartial),
		writeFixture(t, dir, "values_full.yaml", seedValuesFull)
}

func runSeed(t *testing.T, env []string, args ...string) scriptRun {
	t.Helper()
	requireTools(t, "yq", "jq")
	return runScript(t, seedScript, "seed-loopruntime-secrets", env, args...)
}

var seedCounter = regexp.MustCompile(`\b(total|seeded|skipped|failed)=([0-9]+)`)

// seedCounts reads the first value of each counter the script reports.
func seedCounts(r scriptRun) map[string]string {
	counts := map[string]string{}
	for _, m := range seedCounter.FindAllStringSubmatch(r.stderr, -1) {
		if _, seen := counts[m[1]]; !seen {
			counts[m[1]] = m[2]
		}
	}
	return counts
}

func TestSeedLoopRuntimeSecretsRequiresAnEnv(t *testing.T) {
	_, values, _ := seedFiles(t)
	r := runSeed(t, nil, "--secret-values-file", values)
	expectExit(t, r, 1, "env required")
}

func TestSeedLoopRuntimeSecretsRequiresAValuesFile(t *testing.T) {
	r := runSeed(t, nil, "--env", "vault-local")
	expectExit(t, r, 1, "secret-values-file required")
}

func TestSeedLoopRuntimeSecretsRequiresBaoAddrOutsideDryRun(t *testing.T) {
	inv, values, _ := seedFiles(t)
	r := runSeed(t, nil, "--env", "vault-local", "--secret-values-file", values, "--inventory", inv)
	expectExit(t, r, 1, "BAO_ADDR env var required")
}

func TestSeedLoopRuntimeSecretsDryRunCountsEveryKeyAndWritesNothing(t *testing.T) {
	inv, values, _ := seedFiles(t)
	baoLog := filepath.Join(t.TempDir(), "bao.log")
	r := runSeed(t, []string{"FAKE_BAO_LOG=" + baoLog},
		"--env", "vault-local", "--secret-values-file", values, "--inventory", inv, "--dry-run")

	expectExit(t, r, 0)
	want := map[string]string{"total": "5", "seeded": "4", "skipped": "1"}
	got := seedCounts(r)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q\n%s", k, got[k], v, r)
		}
	}
	if c := calls(t, baoLog); len(c) != 0 {
		t.Errorf("a dry run wrote to OpenBao: %v", c)
	}
}

// The tenant operator's GITEA_TOKEN is overridden onto the api's path, so the
// gitea token is written twice under api/ and never under operators/.
func TestSeedLoopRuntimeSecretsWritesAnOverriddenKeyUnderTheOverridingBinary(t *testing.T) {
	inv, _, full := seedFiles(t)
	baoLog := filepath.Join(t.TempDir(), "bao.log")
	r := runSeed(t, append([]string{"FAKE_BAO_LOG=" + baoLog}, seedSession...),
		"--env", "vault-local", "--secret-values-file", full, "--inventory", inv)

	expectExit(t, r, 0)
	if n := countCalls(t, baoLog, "kv put secret/loopruntime/vault-local/api/secrets/gitea/token"); n != 2 {
		t.Errorf("gitea token written %d time(s) under api/, want 2: %v", n, calls(t, baoLog))
	}
	if n := countCalls(t, baoLog, "kv put secret/loopruntime/vault-local/operators/secrets/gitea/token"); n != 0 {
		t.Errorf("gitea token written %d time(s) under operators/, want 0: %v", n, calls(t, baoLog))
	}
}

func TestSeedLoopRuntimeSecretsSkipsAKeyWithNoValueRatherThanFailing(t *testing.T) {
	inv, values, _ := seedFiles(t)
	r := runSeed(t, seedSession, "--env", "vault-local", "--secret-values-file", values, "--inventory", inv)

	expectExit(t, r, 0)
	got := seedCounts(r)
	if got["skipped"] != "1" || got["failed"] != "0" {
		t.Errorf("skipped=%q failed=%q, want 1 and 0\n%s", got["skipped"], got["failed"], r)
	}
}

func TestSeedLoopRuntimeSecretsWritesTheSamePathsOnASecondRun(t *testing.T) {
	inv, _, full := seedFiles(t)
	var runs [2][]string
	for i := range runs {
		baoLog := filepath.Join(t.TempDir(), "bao.log")
		r := runSeed(t, append([]string{"FAKE_BAO_LOG=" + baoLog}, seedSession...),
			"--env", "vault-local", "--secret-values-file", full, "--inventory", inv)
		expectExit(t, r, 0)
		runs[i] = writtenPaths(t, baoLog)
	}
	if len(runs[0]) == 0 {
		t.Fatal("the first run wrote nothing, so there is nothing to compare")
	}
	if !slices.Equal(runs[0], runs[1]) {
		t.Errorf("two runs over the same inventory wrote different paths:\n first:  %v\n second: %v", runs[0], runs[1])
	}
}

func TestSeedLoopRuntimeSecretsCountsEveryFailedWriteAndExitsTwo(t *testing.T) {
	inv, _, full := seedFiles(t)
	r := runSeed(t, append([]string{"FAKE_BAO_EXIT=1"}, seedSession...),
		"--env", "vault-local", "--secret-values-file", full, "--inventory", inv)

	expectExit(t, r, 2)
	if got := seedCounts(r)["failed"]; got != "5" {
		t.Errorf("failed = %q, want 5\n%s", got, r)
	}
}

func TestSeedLoopRuntimeSecretsDryRunNeverPrintsAValue(t *testing.T) {
	inv, _, full := seedFiles(t)
	r := runSeed(t, nil, "--env", "vault-local", "--secret-values-file", full, "--inventory", inv, "--dry-run")
	expectExit(t, r, 0)
	refuseLeak(t, r, seedGiteaValue, seedStripeValue, seedDOValue, seedSpacesValue)
}

var baoValueArg = regexp.MustCompile(`value=[^ ]+`)

// writtenPaths returns the recorded bao calls with their value argument removed,
// sorted — what a run wrote WHERE, independent of what it wrote.
func writtenPaths(t *testing.T, baoLog string) []string {
	t.Helper()
	var paths []string
	for _, c := range calls(t, baoLog) {
		paths = append(paths, baoValueArg.ReplaceAllString(c, ""))
	}
	slices.Sort(paths)
	return paths
}
