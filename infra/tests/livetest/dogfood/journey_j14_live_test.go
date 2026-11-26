// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build live

// T4.5 of plan canonical-tests-rewrite-v2 v1.0 — J-14 Audit Log.

package dogfood

import (
	"context"
	"net/url"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-14 Audit Log — query + filter", Label("live", "dogfood", "j14", "compliance"), func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		env    Env
		jwt    string
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
		env = DefaultEnv()
		var err error
		jwt, err = LoadJWT(env)
		Expect(err).ToNot(HaveOccurred())
		Expect(HealthcheckCloud(ctx, env)).To(Succeed())
	})

	AfterEach(func() { cancel() })

	Context("Scenario A — audit log default page", func() {
		It("GET /engine/api/v1/audit returns 200 with entries + pagination", func() {
			// The audit log is loop-runtime-api's GET /api/v1/audit (entries, total, limit,
			// offset), reached through loop-console's /engine proxy. loop-console
			// serves no /api/v1/audit-log, so the path this spec used answered 404.
			status, body, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/audit?limit=10", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
			Expect(body).To(HaveKey("entries"))
			Expect(body).To(HaveKey("total"))
		})
	})

	// auditPage reads one page of the audit log with the given query.
	auditPage := func(query string) (float64, []map[string]any) {
		status, body, err := CurlJSON(ctx, env, "GET",
			env.CloudBaseURL+"/engine/api/v1/audit?"+query, jwt, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(status).To(Equal(200), "GET /audit?%s", query)
		total, ok := body["total"].(float64)
		Expect(ok).To(BeTrue(), "the answer has no total: %v", body)
		raw, _ := body["entries"].([]any)
		entries := make([]map[string]any, 0, len(raw))
		for _, entry := range raw {
			asMap, _ := entry.(map[string]any)
			entries = append(entries, asMap)
		}
		return total, entries
	}

	Context("Scenario B — action filter", func() {
		It("GET /engine/api/v1/audit?action=project.created returns only that action", func() {
			// Creating the project is the audited event; a tenant that already has
			// it keeps the entry of the day it was created.
			_, err := NewEngine(env, jwt).EnsureProject(ctx, ProbeProject)
			Expect(err).ToNot(HaveOccurred())

			total, entries := auditPage("action=project.created&limit=50")
			Expect(total).To(BeNumerically(">=", 1), "no project.created entry for a tenant that has a project")
			Expect(entries).ToNot(BeEmpty())
			for _, entry := range entries {
				Expect(entry["action"]).To(Equal("project.created"))
			}

			unfiltered, _ := auditPage("limit=1")
			Expect(total).To(BeNumerically("<=", unfiltered))

			none, noEntries := auditPage("action=no.such.action&limit=50")
			Expect(none).To(BeZero())
			Expect(noEntries).To(BeEmpty())
		})
	})

	Context("Scenario C — since filter", func() {
		It("GET /engine/api/v1/audit?since= returns only entries from that moment on", func() {
			_, err := NewEngine(env, jwt).EnsureProject(ctx, ProbeProject)
			Expect(err).ToNot(HaveOccurred())

			_, newest := auditPage("limit=1")
			Expect(newest).To(HaveLen(1))
			stamp, _ := newest[0]["timestamp"].(string)
			newestAt, err := time.Parse(time.RFC3339Nano, stamp)
			Expect(err).ToNot(HaveOccurred(), "the newest entry has timestamp %q", stamp)
			// To the second, so that the newest entry is inside the window however
			// the API compares the bound.
			since := newestAt.Truncate(time.Second)

			total, entries := auditPage("limit=50&since=" + url.QueryEscape(since.Format(time.RFC3339)))
			Expect(total).To(BeNumerically(">=", 1))
			for _, entry := range entries {
				at, err := time.Parse(time.RFC3339Nano, entry["timestamp"].(string))
				Expect(err).ToNot(HaveOccurred())
				Expect(at).To(BeTemporally(">=", since), "entry %v is older than since=%s", entry, since)
			}

			all, _ := auditPage("limit=1")
			Expect(total).To(BeNumerically("<=", all))

			tomorrow := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
			later, laterEntries := auditPage("limit=50&since=" + url.QueryEscape(tomorrow))
			Expect(later).To(BeZero())
			Expect(laterEntries).To(BeEmpty())
		})
	})
})
