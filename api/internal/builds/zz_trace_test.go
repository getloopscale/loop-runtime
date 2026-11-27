package builds

import (
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
)

func TestTrace_ServicesJSONDefault_PathDivergence(t *testing.T) {
	// Simulate a services.json node app in a monorepo subpath, NO explicit build/framework
	// (the canonical LoopZeroKit default: runtime=node lives elsewhere, framework empty here).
	u, err := deploycontracts.NormalizeApp("web", deploycontracts.AppSpec{Name: "web", Type: "server", Path: "apps/web", Port: 8001, Start: "node dist/main.js"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("NormalizeApp: Build=%q Dockerfile=%q Runtime=%q Path=%q", u.Build, u.Dockerfile, u.Runtime, u.Path)
	plan := deploycontracts.CreateBuildPlan([]deploycontracts.AppUnit{u}, "tn", "proj", "sha", "reg.io")
	t.Logf("CreateBuildPlan DockerfilePath=%q ContextPath=%q", plan.Apps[0].DockerfilePath, plan.Apps[0].ContextPath)
}
