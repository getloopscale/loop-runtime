// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package secrets

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

// The bootstrap-user seed writes users, accounts, tenants and tenant_members and
// deletes the user's memberships in every OTHER tenant — cross-tenant work by
// definition. Since 000064 dropped the owner's maintenance policies, the table
// owner is refused by FORCE ROW LEVEL SECURITY; cross-tenant writes go through
// `loopzero_worker` and its named *_worker_cross_tenant policies (000062). Connecting
// as the owner stopped a cold setup:
//
//	ERROR:  new row violates row-level security policy for table "tenants"
//
// These run seed-local-user.sh with faked kubectl and loopzero-ops, and record the
// role psql connected as and the Secret key its password came from.

const seedLocalUserProfile = "seed-local-user-role"

const seedRecordEnv = "LOOPZERO_FAKE_SEED_RECORD"

func init() {
	registerFakes(seedLocalUserProfile, map[string]fakeTool{
		"kubectl": func(args []string) int {
			if len(args) > 0 && strings.HasPrefix(args[0], "--context") {
				args = args[1:]
			}
			record := func(line string) {
				f, err := os.OpenFile(os.Getenv(seedRecordEnv), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
				if err == nil {
					_, _ = f.WriteString(line + "\n")
					_ = f.Close()
				}
			}
			joined := strings.Join(args, " ")
			switch {
			case strings.Contains(joined, "get pod"):
				os.Stdout.WriteString("postgresql-0")
			case strings.Contains(joined, "get secret loopzero-db-credentials"):
				for _, a := range args {
					if key, ok := strings.CutPrefix(a, "jsonpath={.data."); ok {
						key = strings.TrimSuffix(key, "}")
						record("secret-key " + key)
						os.Stdout.WriteString(base64.StdEncoding.EncodeToString([]byte("pw-for-" + key)))
					}
				}
			case strings.Contains(joined, "exec") && strings.Contains(joined, "psql"):
				for i, a := range args {
					if a == "-U" && i+1 < len(args) {
						record("psql-user " + args[i+1])
					}
					if pw, ok := strings.CutPrefix(a, "PGPASSWORD="); ok {
						record("psql-password " + pw)
					}
				}
			}
			return 0
		},
		"loopzero-ops": func(args []string) int {
			os.Stdout.WriteString("SELECT 1;\n")
			return 0
		},
	})
}

func seedRecord(t *testing.T) []string {
	t.Helper()
	record := t.TempDir() + "/seed.log"
	runScript(t, "../../helmfile/scripts/seed-local-user.sh", seedLocalUserProfile,
		[]string{seedRecordEnv + "=" + record})
	raw, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("the seed made no recorded call: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func TestTheBootstrapUserSeedConnectsAsTheWorkerRole(t *testing.T) {
	lines := seedRecord(t)
	var users []string
	for _, l := range lines {
		if u, ok := strings.CutPrefix(l, "psql-user "); ok {
			users = append(users, u)
		}
	}
	if len(users) == 0 {
		t.Fatalf("the seed never ran psql; recorded: %v", lines)
	}
	for _, u := range users {
		if u != "loopzero_worker" {
			t.Errorf("the seed ran psql as %q; the owner is refused by FORCE RLS since 000064 — cross-tenant writes use loopzero_worker", u)
		}
	}
}

func TestTheBootstrapUserSeedUsesTheWorkerPassword(t *testing.T) {
	lines := seedRecord(t)
	for _, l := range lines {
		if pw, ok := strings.CutPrefix(l, "psql-password "); ok && pw != "pw-for-worker-password" {
			t.Errorf("psql got the password %q; want the one stored under worker-password", pw)
		}
	}
}
