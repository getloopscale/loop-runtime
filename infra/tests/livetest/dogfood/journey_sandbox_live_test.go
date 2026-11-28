// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build live

package dogfood

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// SandboxInternalAddrsEnv names what a sandbox must never reach: space-separated
// host:port pairs inside the platform. The defaults are the cloud metadata address,
// the sandbox API and SSH on the gateway of the core VPC, the emulator on its own
// network, the host on Docker's bridge and the bridge of the kind clusters.
const SandboxInternalAddrsEnv = "LOOP_RUNTIME_DOGFOOD_SANDBOX_INTERNAL_ADDRS"

// A customer's code sandboxes, through loop-console's /v1/sandboxes with the token of
// the validation identity: the credential a customer already holds, and nothing of the
// sandbox service's own.
var _ = Describe("Sandbox — code sandboxes with the LoopScale credential", Label("live", "dogfood", "sandbox"), Ordered, func() {
	var (
		env    Env
		jwt    string
		client *http.Client
		base   string
	)

	// call sends one request and decodes a JSON object answer, when there is one.
	call := func(method, path string, body any) (int, map[string]any) {
		GinkgoHelper()
		var payload io.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			Expect(err).ToNot(HaveOccurred())
			payload = bytes.NewReader(raw)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, method, base+path, payload)
		Expect(err).ToNot(HaveOccurred())
		req.Header.Set("Authorization", "Bearer "+jwt)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		Expect(err).ToNot(HaveOccurred(), "%s %s", method, path)
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		Expect(err).ToNot(HaveOccurred())
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		if out == nil {
			out = map[string]any{"raw": string(raw)}
		}
		return resp.StatusCode, out
	}

	create := func(network map[string]any) string {
		GinkgoHelper()
		body := map[string]any{"image": "python:3.12-slim", "ttl_seconds": 900, "metadata": map[string]string{"journey": "sandbox"}}
		if network != nil {
			body["network"] = network
		}
		status, sb := call(http.MethodPost, "/v1/sandboxes", body)
		Expect(status).To(Equal(http.StatusCreated), "create sandbox: %v", sb)
		Expect(sb["state"]).To(Equal("running"))
		id, _ := sb["id"].(string)
		Expect(id).ToNot(BeEmpty())
		DeferCleanup(func() { call(http.MethodDelete, "/v1/sandboxes/"+id, nil) })
		return id
	}

	// run executes a command and returns its exit code and combined output.
	run := func(id string, cmd ...string) (int, string) {
		GinkgoHelper()
		status, res := call(http.MethodPost, "/v1/sandboxes/"+id+"/executions", map[string]any{"cmd": cmd, "timeout_ms": 300000})
		Expect(status).To(Equal(http.StatusOK), "exec %v: %v", cmd, res)
		code, ok := res["exit_code"].(float64)
		Expect(ok).To(BeTrue(), "no exit_code in %v", res)
		return int(code), fmt.Sprint(res["stdout"], res["stderr"])
	}

	fetch := func(id, url string) int {
		GinkgoHelper()
		code, _ := run(id, "python3", "-c", fmt.Sprintf("import urllib.request; urllib.request.urlopen(%q, timeout=15)", url))
		return code
	}

	denials := func(id string) []map[string]any {
		GinkgoHelper()
		status, out := call(http.MethodGet, "/v1/sandboxes/"+id+"/network/denials", nil)
		Expect(status).To(Equal(http.StatusOK), "list denials: %v", out)
		list, _ := out["denials"].([]any)
		rows := make([]map[string]any, 0, len(list))
		for _, d := range list {
			if row, ok := d.(map[string]any); ok {
				rows = append(rows, row)
			}
		}
		return rows
	}

	BeforeAll(func() {
		env = DefaultEnv()
		base = env.AppBaseURL
		client = &http.Client{Timeout: 6 * time.Minute}
		var err error
		jwt, err = LoadJWT(env)
		Expect(err).ToNot(HaveOccurred(), "JWT seed required")
	})

	It("refuses a caller with no credential", func() {
		req, err := http.NewRequest(http.MethodGet, base+"/v1/sandboxes", nil)
		Expect(err).ToNot(HaveOccurred())
		resp, err := client.Do(req)
		Expect(err).ToNot(HaveOccurred())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
	})

	It("says the deployment gives sandboxes a network", func() {
		status, caps := call(http.MethodGet, "/v1/capabilities", nil)
		Expect(status).To(Equal(http.StatusOK), "capabilities: %v", caps)
		Expect(caps["driver"]).To(Equal("container"))
		supported := map[string]bool{}
		list, _ := caps["capabilities"].([]any)
		for _, c := range list {
			if row, ok := c.(map[string]any); ok {
				supported[fmt.Sprint(row["name"])], _ = row["supported"].(bool)
			}
		}
		for _, name := range []string{"exec", "files", "sessions", "pty", "network"} {
			Expect(supported[name]).To(BeTrue(), "capability %s", name)
		}
	})

	Context("a sandbox with no rules", Ordered, func() {
		var id string

		BeforeAll(func() { id = create(nil) })

		It("is listed for its tenant", func() {
			status, out := call(http.MethodGet, "/v1/sandboxes", nil)
			Expect(status).To(Equal(http.StatusOK))
			var ids []string
			list, _ := out["sandboxes"].([]any)
			for _, s := range list {
				if row, ok := s.(map[string]any); ok {
					ids = append(ids, fmt.Sprint(row["id"]))
				}
			}
			Expect(ids).To(ContainElement(id))
		})

		It("installs pandas with a plain pip install and uses it", func() {
			code, out := run(id, "pip", "install", "pandas")
			Expect(code).To(Equal(0), "pip install pandas: %s", out)

			code, out = run(id, "python3", "-c", `import pandas; print(pandas.DataFrame({"a": [1, 2]}).a.sum())`)
			Expect(code).To(Equal(0), "import pandas: %s", out)
			Expect(strings.TrimSpace(out)).To(Equal("3"))
		})

		It("never reaches an address inside the platform", func() {
			addrs := strings.Fields(envOr(SandboxInternalAddrsEnv, "169.254.169.254:80 10.40.0.1:8090 10.40.0.1:22 172.29.0.1:9597 172.17.0.1:22 172.18.0.1:6443"))
			// The guard accepts the connection itself and closes it, so a connect that
			// succeeds proves nothing. What counts is that no byte of the destination
			// comes back: the probe exits 3 when one does.
			const probe = `import socket, sys
try:
    s = socket.create_connection((%q, %s), timeout=6)
    s.sendall(b"GET / HTTP/1.0\r\n\r\n")
    data = s.recv(1)
except OSError:
    sys.exit(0)
sys.exit(3 if data else 0)`
			for _, addr := range addrs {
				host, port, _ := strings.Cut(addr, ":")
				code, out := run(id, "python3", "-c", fmt.Sprintf(probe, host, port))
				Expect(code).To(Equal(0), "the sandbox got an answer from %s: %s", addr, out)
			}
			refused := map[string]bool{}
			for _, d := range denials(id) {
				refused[fmt.Sprint(d["destination"])] = true
			}
			for _, addr := range addrs {
				Expect(refused[addr]).To(BeTrue(), "no denial recorded for %s", addr)
			}
		})

		It("is gone after it is destroyed", func() {
			status, _ := call(http.MethodDelete, "/v1/sandboxes/"+id, nil)
			Expect(status).To(Equal(http.StatusNoContent))
			status, _ = call(http.MethodGet, "/v1/sandboxes/"+id, nil)
			Expect(status).To(Equal(http.StatusNotFound))
		})
	})

	Context("a sandbox restricted to pypi.org", Ordered, func() {
		var id string

		BeforeAll(func() {
			id = create(map[string]any{"allow_out": []string{"pypi.org"}, "deny_out": []string{"0.0.0.0/0"}})
		})

		It("reaches pypi.org and is refused example.com", func() {
			Expect(fetch(id, "https://pypi.org/simple/pip/")).To(Equal(0), "pypi.org is in allow_out")
			Expect(fetch(id, "https://example.com/")).ToNot(Equal(0), "example.com is outside allow_out")
		})

		It("records the refused connection", func() {
			var hosts []string
			for _, d := range denials(id) {
				hosts = append(hosts, fmt.Sprint(d["host"]))
			}
			Expect(hosts).To(ContainElement("example.com"))
		})

		It("follows new rules while it runs", func() {
			status, applied := call(http.MethodPut, "/v1/sandboxes/"+id+"/network",
				map[string]any{"allow_out": []string{"pypi.org", "example.com"}, "deny_out": []string{"0.0.0.0/0"}})
			Expect(status).To(Equal(http.StatusOK), "update network: %v", applied)

			Expect(fetch(id, "https://example.com/")).To(Equal(0), "example.com was added to allow_out")
			Expect(fetch(id, "https://example.org/")).ToNot(Equal(0), "example.org is still outside allow_out")
		})
	})
})
