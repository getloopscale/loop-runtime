// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build live

// The app lifecycle journey: a real app, deployed and operated through the
// public API the way a user does it, against the environment the suite is
// pointed at (staging, from infra/staging/validate.sh).
//
// It fails when any of these stops working:
//
//	step 1  a source upload builds, releases and serves on its public URL
//	step 2  two secrets of one app both reach it after a redeploy of the
//	        same image
//	step 3  a scale through the API changes the running Rollout
//	step 4  a line the app logs arrives on the runtime log stream
//	step 5  the metrics endpoint answers the app's ready replicas
//
// The app is fixtures/env-probe, a node server with no dependency. The journey
// uses its own project (ProbeProject) and creates what it needs, so it runs on
// an empty tenant and on what an earlier run left behind. On a plan that
// allows one project it deploys to the project that holds the slot
// (Engine.EnsureProject). It deletes nothing:
// a failed run stays on the cluster to be looked at, and the next run reuses
// the project. Every wait has a deadline and names its step when it passes.
//
// The values of the secrets are random, live in this process only and are
// never printed: the app answers the SHA-256 of a variable, and the journey
// compares digests.

package dogfood

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	// journeyBudget bounds the whole journey. infra/staging/validate.sh gives
	// the suite a longer go test timeout, so a journey that runs out of time
	// fails here, naming its step.
	journeyBudget = 25 * time.Minute

	buildTimeout       = 12 * time.Minute // a first build pulls the base image and pushes the app's
	releaseTimeout     = 5 * time.Minute  // sync, canary steps and the public route
	scaleTimeout       = 2 * time.Minute
	logStreamTimeout   = 60 * time.Second
	metricsTimeout     = 4 * time.Minute // the cells record the series every minute and the core federates it
	pollInterval       = 5 * time.Second
	secondSecretName   = "PROBE_BETA"
	firstSecretName    = "PROBE_ALPHA"
	replicasReadyQuery = "replicas_ready"
)

var _ = Describe("App lifecycle", Ordered, Label("live", "dogfood", "app-lifecycle"), func() {
	var (
		ctx     context.Context
		cancel  context.CancelFunc
		env     Env
		engine  *Engine
		target  ProbeTarget
		appURL  string
		buildID string
	)

	// must fails the journey with the step's own message. The error of a
	// helper already names the request and the answer.
	must := func(step string, err error) {
		GinkgoHelper()
		if err == nil {
			return
		}
		if step == "" { // a wait already names its step
			Fail(err.Error())
		}
		Fail(fmt.Sprintf("%s: %v", step, err))
	}

	// deploy submits a build and waits until its release is the active one.
	deploy := func(step string, request map[string]any) string {
		GinkgoHelper()
		id, err := engine.SubmitBuild(ctx, request)
		must(step+": submit the build", err)
		By(fmt.Sprintf("%s: build %s submitted", step, id))

		must("", WaitFor(ctx, step+": build "+id+" succeeds", buildTimeout, pollInterval, func(ctx context.Context) (bool, string) {
			build, err := engine.GetBuild(ctx, id)
			if err != nil {
				return false, err.Error()
			}
			switch BuildOutcome(build.Status) {
			case "succeeded":
				return true, ""
			case "failed":
				Fail(fmt.Sprintf("%s: build %s ended %s: %s", step, id, build.Status, build.Error))
			}
			return false, "build status " + build.Status
		}))

		must("", WaitFor(ctx, step+": the release of build "+id+" becomes the active one", releaseTimeout, pollInterval, func(ctx context.Context) (bool, string) {
			status, err := engine.Status(ctx, target.Name, ProbeEnvironment)
			if err != nil {
				return false, err.Error()
			}
			if status.ActiveRelease == nil {
				return false, "no active release, sync " + status.SyncStatus
			}
			if status.ActiveRelease.BuildID != id {
				return false, "the active release is of build " + status.ActiveRelease.BuildID
			}
			appURL = status.URL
			return appURL != "", "the active release has no URL"
		}))
		return id
	}

	BeforeAll(func() {
		ctx, cancel = context.WithTimeout(context.Background(), journeyBudget)
		DeferCleanup(func() { cancel() })
		env = DefaultEnv()
		jwt, err := LoadJWT(env)
		Expect(err).ToNot(HaveOccurred(), "the validation identity's token")
		Expect(HealthcheckCloud(ctx, env)).To(Succeed(), "deep health before the journey")
		engine = NewEngine(env, jwt)
	})

	It("step 1: deploys an app from a source upload and serves it on its public URL", func() {
		var err error
		target, err = engine.EnsureProject(ctx, ProbeProject)
		must("step 1: create the project "+ProbeProject, err)
		if target.Reused {
			By("step 1: the plan refused a second project; deploying to " + target.Name + ", the one the tenant has")
		}

		manifest, err := ProbeAppManifest(target.Name)
		must("step 1: read the app's manifest", err)
		bundle, err := ProbeAppBundle(target.Name)
		must("step 1: bundle the app", err)
		sourceID, err := engine.UploadSource(ctx, manifest, bundle)
		must("step 1: upload the source", err)
		By("step 1: source " + sourceID + " uploaded")

		buildID = deploy("step 1", map[string]any{
			"project_id":  target.Name,
			"environment": ProbeEnvironment,
			"commit_sha":  sourceID,
			"apps": []map[string]any{{
				"name": ProbeApp, "type": "server", "runtime": "node",
				"port": ProbePort, "start": "node server.js",
			}},
		})

		must("", WaitFor(ctx, "step 1: GET "+appURL+"/health answers 200", releaseTimeout, pollInterval, func(ctx context.Context) (bool, string) {
			status, err := CallApp(ctx, env.HTTPClient, appURL, "/health")
			if err != nil {
				return false, err.Error()
			}
			return status == http.StatusOK, fmt.Sprintf("HTTP %d", status)
		}))
	})

	It("step 2: delivers two secrets of one app after a redeploy of the same image", func() {
		Expect(buildID).ToNot(BeEmpty(), "step 2 needs the build of step 1")
		digests := map[string]string{}
		for _, name := range []string{firstSecretName, secondSecretName} {
			value, err := NewSecretValue()
			must("step 2: generate the value of "+name, err)
			digests[name] = SecretDigest(value)
			must("step 2: set the secret "+name, engine.SetSecret(ctx, target.Name, ProbeEnvironment, ProbeApp, name, value))
		}

		// The source did not change, so this build reuses the image: the pods
		// roll only because the release does.
		deploy("step 2", map[string]any{"redeploy_of": buildID})

		for _, name := range []string{firstSecretName, secondSecretName} {
			want := digests[name]
			must("", WaitFor(ctx, "step 2: the app reads "+name+" with the value just set", releaseTimeout, pollInterval, func(ctx context.Context) (bool, string) {
				answer, err := ProbeVariable(ctx, env.HTTPClient, appURL, name)
				if err != nil {
					return false, err.Error()
				}
				if !answer.Present || answer.SHA256 == nil {
					return false, "present=false: the variable is not set in the app"
				}
				if *answer.SHA256 != want {
					return false, "present=true with the digest of another value: the app runs with an older secret"
				}
				return true, ""
			}))
		}
	})

	It("step 3: scales the running Rollout through the API", func() {
		Expect(target.TenantID).ToNot(BeEmpty(), "step 3 needs the project of step 1")
		namespace := TenantNamespace(target.TenantID, target.Name, ProbeEnvironment)
		rollout := target.Name + "-" + ProbeApp
		// Back to one replica whatever happens, so a failed run does not leave
		// the tenant at its plan's ceiling. The failure of the step stands.
		DeferCleanup(func() {
			restore, cancelRestore := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancelRestore()
			if err := engine.Scale(restore, target.Name, ProbeEnvironment, ProbeApp, 1); err != nil {
				GinkgoWriter.Printf("cleanup: could not scale %s back to 1 replica: %v\n", target.Name, err)
			}
		})
		for _, replicas := range []int{2, 1} {
			step := fmt.Sprintf("step 3: scale to %d", replicas)
			must(step, engine.Scale(ctx, target.Name, ProbeEnvironment, ProbeApp, replicas))
			must("", WaitFor(ctx, fmt.Sprintf("%s: spec.replicas of Rollout %s/%s is %d", step, namespace, rollout, replicas), scaleTimeout, pollInterval, func(ctx context.Context) (bool, string) {
				got, cell, err := RolloutReplicas(ctx, HostKubectl, CellContexts(), namespace, rollout)
				if err != nil {
					return false, err.Error()
				}
				return got == replicas, fmt.Sprintf("spec.replicas is %d on %s", got, cell)
			}))
		}
	})

	It("step 4: returns a line the app logged on the runtime log stream", func() {
		Expect(appURL).ToNot(BeEmpty(), "step 4 needs the app of step 1")
		marker, err := NewSecretValue()
		must("step 4: generate the marker", err)
		marker = "mk-" + marker[:24]
		status, err := CallApp(ctx, env.HTTPClient, appURL, "/health?marker="+marker)
		must("step 4: call the app with the marker", err)
		Expect(status).To(Equal(http.StatusOK), "step 4: the app answers the marked request")

		last := "the stream was not opened"
		must("", WaitFor(ctx, "step 4: the line with marker "+marker+" arrives on GET /projects/"+target.Name+"/logs/stream", logStreamTimeout, 2*time.Second, func(ctx context.Context) (bool, string) {
			// One stream at a time, each for at most 20 s: a stream that
			// stays open without the line must not use the whole minute
			// on a connection opened before the line was ingested.
			streamCtx, cancelStream := context.WithTimeout(ctx, 20*time.Second)
			defer cancelStream()
			found, err := engine.LogStreamHas(streamCtx, target.Name, ProbeEnvironment, "5m", marker)
			if err != nil {
				last = err.Error()
			} else if !found {
				last = "the stream answered 200 without the line"
			}
			return found, last
		}))
	})

	It("step 5: answers the app's ready replicas on the metrics endpoint", func() {
		must("", WaitFor(ctx, "step 5: GET /projects/"+target.Name+"/metrics?signal="+replicasReadyQuery+"&range=1h answers status ok with a ready count", metricsTimeout, 10*time.Second, func(ctx context.Context) (bool, string) {
			series, err := engine.Metric(ctx, target.Name, replicasReadyQuery, "1h")
			if err != nil {
				return false, err.Error()
			}
			if !strings.EqualFold(series.Status, "ok") {
				return false, fmt.Sprintf("status %q, hint %q", series.Status, series.CollectingHint)
			}
			if len(series.Data) == 0 {
				return false, "status ok with no data point"
			}
			latest := series.Data[len(series.Data)-1].V
			return latest >= 1, fmt.Sprintf("the latest ready count is %v", latest)
		}))
	})
})
