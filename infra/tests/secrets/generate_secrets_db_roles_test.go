// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package secrets

import (
	"net/url"
	"os"
	"strings"
	"testing"
)

// loopruntime-api refuses to boot on a request pool that can bypass RLS
// (db.AssertRequestPoolIsolated), so its DATABASE_URL must be the non-owner
// `loopruntime_api` role and its background pool `loopruntime_worker`. Until #396 the
// credentials Secret carried one URL, the table owner's, and every fresh install
// produced an API that exited at boot. These tests run generate-secrets.sh for
// real — only kubectl is faked — and read what it writes into the Secret.

const dbRolesProfile = "generate-secrets-db-roles"

// secretRecordEnv names the file the fake kubectl appends each created Secret's
// arguments to, one `name<TAB>arg` line per --from-literal.
const secretRecordEnv = "LOOPRUNTIME_FAKE_SECRET_RECORD"

func init() {
	registerFakes(dbRolesProfile, map[string]fakeTool{
		"kubectl": func(args []string) int {
			// The script calls kubectl through a --context wrapper; drop it.
			if len(args) > 0 && strings.HasPrefix(args[0], "--context") {
				args = args[1:]
			}
			if len(args) >= 3 && args[0] == "create" && args[1] == "secret" {
				name := args[3]
				f, err := os.OpenFile(os.Getenv(secretRecordEnv), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
				if err != nil {
					return 1
				}
				defer f.Close()
				for _, a := range args {
					if v, ok := strings.CutPrefix(a, "--from-literal="); ok {
						_, _ = f.WriteString(name + "\t" + v + "\n")
					}
				}
				return 0
			}
			if len(args) > 0 && args[0] == "get" {
				return 1 // nothing exists yet: a cold bootstrap
			}
			return 0 // apply, create namespace, label, annotate: accepted
		},
	})
}

// dbCredentials runs the script and returns the literals it put into
// loopruntime-system/loopruntime-db-credentials.
func dbCredentials(t *testing.T, env ...string) map[string]string {
	t.Helper()
	record := t.TempDir() + "/secrets.tsv"
	run := runScript(t, "../../helmfile/scripts/generate-secrets.sh", dbRolesProfile,
		append([]string{secretRecordEnv + "=" + record}, env...), "standalone", "core")
	if run.code != 0 {
		t.Fatalf("generate-secrets.sh failed:\n%s", run)
	}
	raw, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("the script created no Secret at all: %v\n%s", err, run)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		name, literal, _ := strings.Cut(line, "\t")
		if name != "loopruntime-db-credentials" {
			continue
		}
		key, value, _ := strings.Cut(literal, "=")
		got[key] = value
	}
	if len(got) == 0 {
		t.Fatalf("loopruntime-db-credentials was never created:\n%s", run)
	}
	return got
}

func roleOf(t *testing.T, creds map[string]string, key string) string {
	t.Helper()
	raw, ok := creds[key]
	if !ok {
		t.Fatalf("loopruntime-db-credentials has no %q key; keys: %v", key, keysOf(creds))
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("%s is not a URL: %v", key, err)
	}
	return u.User.Username()
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestTheRequestPoolURLConnectsAsTheNonOwnerRole(t *testing.T) {
	creds := dbCredentials(t)
	if got := roleOf(t, creds, "api-uri"); got != "loopruntime_api" {
		t.Errorf("api-uri connects as %q, want loopruntime_api — loopruntime-api refuses to serve requests as any role that can bypass RLS", got)
	}
}

func TestTheBackgroundPoolURLConnectsAsTheWorkerRole(t *testing.T) {
	creds := dbCredentials(t)
	if got := roleOf(t, creds, "worker-uri"); got != "loopruntime_worker" {
		t.Errorf("worker-uri connects as %q, want loopruntime_worker", got)
	}
}

// The control plane connects as `loopruntime_cloud`, the non-owner role migration 000063
// created for it: loopruntime-cloud refuses to serve as a role holding an unnamed
// cross-tenant privilege — superuser, BYPASSRLS, or table owner (#438).
func TestTheControlPlaneURLConnectsAsTheCloudRole(t *testing.T) {
	creds := dbCredentials(t)
	if got := roleOf(t, creds, "cloud-uri"); got != "loopruntime_cloud" {
		t.Errorf("cloud-uri connects as %q, want loopruntime_cloud — loopruntime-cloud refuses to serve as the owner", got)
	}
}

// The owner URL stays: migrations run as the owner.
func TestTheOwnerURLIsKeptForMigrations(t *testing.T) {
	creds := dbCredentials(t)
	if got := roleOf(t, creds, "uri"); got != "loopruntime" {
		t.Errorf("uri connects as %q, want the owner loopruntime", got)
	}
}

func TestTheRolePasswordsCanBeOverridden(t *testing.T) {
	creds := dbCredentials(t, "LOOPRUNTIME_DB_API_PASSWORD=api-override", "LOOPRUNTIME_DB_WORKER_PASSWORD=worker-override", "LOOPRUNTIME_DB_CLOUD_PASSWORD=cloud-override")
	if creds["api-password"] != "api-override" || creds["worker-password"] != "worker-override" {
		t.Errorf("overrides ignored: api-password=%q worker-password=%q", creds["api-password"], creds["worker-password"])
	}
	if creds["cloud-password"] != "cloud-override" || !strings.Contains(creds["cloud-uri"], ":cloud-override@") {
		t.Errorf("cloud override ignored: cloud-password=%q", creds["cloud-password"])
	}
	if !strings.Contains(creds["api-uri"], ":api-override@") {
		t.Errorf("api-uri does not carry the api password it declares")
	}
}
