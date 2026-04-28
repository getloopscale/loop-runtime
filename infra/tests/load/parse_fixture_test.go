package load

import (
	"testing"
)

func TestParseFixtureApps_DogfoodMonorepo(t *testing.T) {
	apps, err := parseFixtureApps("../e2e/fixtures/dogfood-monorepo")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	wantNames := []string{"api", "gateway", "web", "worker"}
	if len(apps) != len(wantNames) {
		t.Fatalf("got %d apps, want %d (names=%+v)", len(apps), len(wantNames), apps)
	}
	for i, want := range wantNames {
		if apps[i].Name != want {
			t.Errorf("apps[%d].Name = %q, want %q", i, apps[i].Name, want)
		}
	}
	for _, a := range apps {
		t.Logf("  %s type=%s framework=%s path=%s port=%d", a.Name, a.Type, a.Framework, a.Path, a.Port)
	}
}

func TestParseFixtureApps_HelloServer(t *testing.T) {
	apps, err := parseFixtureApps("../e2e/fixtures/hello-server")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(apps) != 1 || apps[0].Name != "api" {
		t.Fatalf("got %+v, want 1 app named 'api'", apps)
	}
	if apps[0].Port != 3000 {
		t.Errorf("apps[0].Port = %d, want 3000", apps[0].Port)
	}
}
