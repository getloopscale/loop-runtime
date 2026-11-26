// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package kubeconformschema

import (
	"bytes"
	"errors"
	"testing"
)

const widgetCRD = `---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.runtime.getloopscale.com
spec:
  group: runtime.getloopscale.com
  names:
    kind: Widget
    plural: widgets
  scope: Namespaced
  versions:
  - name: v1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        description: Widget is a test kind — with an em dash.
        properties:
          spec:
            description: Size must be < 10 & > 0.
            properties:
              size:
                maximum: 9
                type: integer
            required:
            - size
            type: object
          apiVersion:
            type: string
        type: object
`

// The format is the one every committed schema already has: a fixed wrapper
// order, keys sorted below it, non-ASCII escaped, and no HTML escaping.
func TestFromCRDWritesTheStrictKubeconformFormat(t *testing.T) {
	got, err := FromCRD([]byte(widgetCRD))
	if err != nil {
		t.Fatalf("FromCRD: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d schemas, want 1", len(got))
	}
	if got[0].Name != "widget-runtime.getloopscale.com-v1.json" {
		t.Errorf("name = %q, want widget-runtime.getloopscale.com-v1.json", got[0].Name)
	}
	want := `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object",` +
		`"properties":{"apiVersion":{"type":"string"},"spec":{"description":"Size must be < 10 & > 0.",` +
		`"properties":{"size":{"maximum":9,"type":"integer"}},"required":["size"],"type":"object"}},` +
		`"required":[],"additionalProperties":false,"description":"Widget is a test kind \u2014 with an em dash."}`
	if string(got[0].Content) != want {
		t.Errorf("content mismatch\n got: %s\nwant: %s", got[0].Content, want)
	}
}

func TestFromCRDIsDeterministic(t *testing.T) {
	a, err := FromCRD([]byte(widgetCRD))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		b, err := FromCRD([]byte(widgetCRD))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a[0].Content, b[0].Content) {
			t.Fatalf("run %d produced different bytes", i)
		}
	}
}

func TestFromCRDEmitsOneSchemaPerVersion(t *testing.T) {
	two := widgetCRD + `  - name: v2
    served: true
    storage: false
    schema:
      openAPIV3Schema:
        properties:
          kind:
            type: string
        required:
        - kind
        type: object
`
	got, err := FromCRD([]byte(two))
	if err != nil {
		t.Fatalf("FromCRD: %v", err)
	}
	if len(got) != 2 || got[1].Name != "widget-runtime.getloopscale.com-v2.json" {
		t.Fatalf("got %+v, want v1 and v2 schemas", names(got))
	}
	want := `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"kind":{"type":"string"}},"required":["kind"],"additionalProperties":false}`
	if string(got[1].Content) != want {
		t.Errorf("v2 content\n got: %s\nwant: %s", got[1].Content, want)
	}
}

func TestFromCRDRefusesADocumentThatIsNotACRD(t *testing.T) {
	_, err := FromCRD([]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n"))
	if !errors.Is(err, ErrNotACRD) {
		t.Fatalf("err = %v, want ErrNotACRD", err)
	}
}

func TestFromCRDRefusesAVersionWithoutASchema(t *testing.T) {
	crd := `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
spec:
  group: runtime.getloopscale.com
  names:
    kind: Widget
  versions:
  - name: v1
`
	_, err := FromCRD([]byte(crd))
	if !errors.Is(err, ErrNoSchema) {
		t.Fatalf("err = %v, want ErrNoSchema", err)
	}
}

func TestFromCRDRefusesACRDWithoutKindOrGroup(t *testing.T) {
	crd := `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
spec:
  versions:
  - name: v1
    schema:
      openAPIV3Schema:
        properties: {}
        type: object
`
	_, err := FromCRD([]byte(crd))
	if !errors.Is(err, ErrIncompleteCRD) {
		t.Fatalf("err = %v, want ErrIncompleteCRD", err)
	}
}

func names(s []Schema) []string {
	out := make([]string, 0, len(s))
	for _, x := range s {
		out = append(out, x.Name)
	}
	return out
}
