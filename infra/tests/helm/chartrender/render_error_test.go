//go:build helm_render

package chartrender

import (
	"strings"
	"testing"
)

// A test whose assertion IS that a render fails (a fail-closed template guard)
// needs the error back instead of a t.Fatal. Going through the facade keeps the
// dependency resolution and the chart-name guard on that path too.
func TestRenderTemplateE_returns_the_helm_error_instead_of_failing(t *testing.T) {
	chartPath, err := PublishedChartPath("loopzero-api")
	if err != nil {
		t.Fatalf("PublishedChartPath: %v", err)
	}
	opts, err := NewOptions(chartPath, "")
	if err != nil {
		t.Fatalf("NewOptions: %v", err)
	}
	// secrets.create=false with no existingSecret is refused by the chart's
	// own `fail` guard — the render must come back as an error.
	opts.SetValues = map[string]string{"secrets.create": "false"}

	_, renderErr := RenderTemplateE(t, opts, chartPath, "fail-closed-probe", nil)

	if renderErr == nil {
		t.Fatal("RenderTemplateE returned no error for a render the chart refuses")
	}
}

// helm is a declared prerequisite. A missing binary must surface as an error
// naming it, so every render test fails the same way instead of some skipping
// (green over nothing) and some fataling.
func TestNewOptions_refuses_when_helm_is_not_on_PATH(t *testing.T) {
	chartPath, err := ChartPath("openbao-cert")
	if err != nil {
		t.Fatalf("ChartPath: %v", err)
	}
	t.Setenv("PATH", t.TempDir())

	_, err = NewOptions(chartPath, "")

	if err == nil || !strings.Contains(err.Error(), "helm") {
		t.Fatalf("NewOptions error = %v, want an error naming the missing helm binary", err)
	}
}
