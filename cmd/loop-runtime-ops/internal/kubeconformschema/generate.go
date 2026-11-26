// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package kubeconformschema derives the strict kubeconform schemas for LoopRuntime's
// own CRDs from the CRDs themselves.
//
// infra/kubeconform-schemas/master-standalone-strict/ holds one JSON schema
// per kind, looked up by kubeconform through the template
// `{{.ResourceKind}}-{{.Group}}-{{.ResourceAPIVersion}}.json`. The runtime.getloopscale.com files
// among them used to be written by a step nobody recorded, so a CRD change that
// skipped it left the schema describing an API the cluster no longer served
// (#389). This package is that step, checked in and deterministic.
//
// The transform is the one the committed files were measured to follow
// (#389): the version's openAPIV3Schema, wrapped at the root as
//
//	{"$schema": draft-07, "type": "object", "properties": ..., "required": [...],
//	 "additionalProperties": false, "description": ...}
//
// with every key below the root sorted, no whitespace, non-ASCII escaped as
// \uXXXX and no HTML escaping. Only the root forbids additional properties,
// which is what kubeconform's strict mode needs to reject an unknown top-level
// field; the nested objects keep the CRD's own structural schema.
//
// kubeconform's own openapi2jsonschema.py produces a different shape and is
// not in this repository's toolchain; keeping the format the committed files
// already had makes a regeneration show only what the CRDs changed.
package kubeconformschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// jsonSchemaDraft is the dialect every generated schema declares.
const jsonSchemaDraft = "http://json-schema.org/draft-07/schema#"

// crdKind is the only document kind FromCRD accepts.
const crdKind = "CustomResourceDefinition"

var (
	// ErrNotACRD is returned for a YAML document that is not a CustomResourceDefinition.
	ErrNotACRD = errors.New("document is not a CustomResourceDefinition")
	// ErrIncompleteCRD is returned for a CRD that names no kind, group or version.
	ErrIncompleteCRD = errors.New("CRD is incomplete")
	// ErrNoSchema is returned for a CRD version that carries no openAPIV3Schema
	// with root properties — there is nothing to validate a manifest against.
	ErrNoSchema = errors.New("CRD version has no openAPIV3Schema properties")
)

// Schema is one generated file: its name in the schema directory and its bytes.
type Schema struct {
	Name    string
	Group   string
	Content []byte
}

type crdDocument struct {
	Kind string `yaml:"kind"`
	Spec struct {
		Group string `yaml:"group"`
		Names struct {
			Kind string `yaml:"kind"`
		} `yaml:"names"`
		Versions []struct {
			Name   string `yaml:"name"`
			Schema struct {
				OpenAPIV3Schema map[string]any `yaml:"openAPIV3Schema"`
			} `yaml:"schema"`
		} `yaml:"versions"`
	} `yaml:"spec"`
}

// strictRoot is the root wrapper. Its field order IS the file format, which is
// why it is a struct: a map would sort "$schema" and "additionalProperties"
// among the rest.
type strictRoot struct {
	Schema               string         `json:"$schema"`
	Type                 string         `json:"type"`
	Properties           map[string]any `json:"properties"`
	Required             []any          `json:"required"`
	AdditionalProperties bool           `json:"additionalProperties"`
	Description          string         `json:"description,omitempty"`
}

// FromCRD returns one strict schema per version of the CRD in data.
func FromCRD(data []byte) ([]Schema, error) {
	var doc crdDocument
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse CRD: %w", err)
	}
	if doc.Kind != crdKind {
		return nil, fmt.Errorf("%w: kind %q", ErrNotACRD, doc.Kind)
	}
	kind, group := doc.Spec.Names.Kind, doc.Spec.Group
	if kind == "" || group == "" || len(doc.Spec.Versions) == 0 {
		return nil, fmt.Errorf("%w: kind=%q group=%q versions=%d", ErrIncompleteCRD, kind, group, len(doc.Spec.Versions))
	}
	out := make([]Schema, 0, len(doc.Spec.Versions))
	for _, v := range doc.Spec.Versions {
		if v.Name == "" {
			return nil, fmt.Errorf("%w: %s has a version with no name", ErrIncompleteCRD, kind)
		}
		content, err := render(v.Schema.OpenAPIV3Schema)
		if err != nil {
			return nil, fmt.Errorf("%s %s: %w", kind, v.Name, err)
		}
		out = append(out, Schema{
			Name:    FileName(kind, group, v.Name),
			Group:   group,
			Content: content,
		})
	}
	return out, nil
}

// FileName is the name kubeconform looks the schema up by.
func FileName(kind, group, version string) string {
	return fmt.Sprintf("%s-%s-%s.json", strings.ToLower(kind), group, version)
}

func render(openAPI map[string]any) ([]byte, error) {
	props, ok := openAPI["properties"].(map[string]any)
	if !ok {
		return nil, ErrNoSchema
	}
	root := strictRoot{
		Schema:     jsonSchemaDraft,
		Type:       "object",
		Properties: props,
		Required:   []any{},
	}
	if req, ok := openAPI["required"].([]any); ok {
		root.Required = req
	}
	if desc, ok := openAPI["description"].(string); ok {
		root.Description = desc
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(root); err != nil {
		return nil, fmt.Errorf("encode schema: %w", err)
	}
	return escapeNonASCII(bytes.TrimSuffix(buf.Bytes(), []byte("\n"))), nil
}

// escapeNonASCII rewrites every non-ASCII rune as a \uXXXX escape (a UTF-16
// surrogate pair above the BMP). encoding/json emits valid JSON, so a
// non-ASCII byte can only occur inside a string literal, where the escape is
// equivalent — it keeps the files pure ASCII, as they have always been.
func escapeNonASCII(in []byte) []byte {
	var out bytes.Buffer
	out.Grow(len(in))
	for len(in) > 0 {
		r, size := utf8.DecodeRune(in)
		in = in[size:]
		if r < utf8.RuneSelf {
			out.WriteRune(r)
			continue
		}
		if r > 0xFFFF {
			hi, lo := utf16.EncodeRune(r)
			fmt.Fprintf(&out, `\u%04x\u%04x`, hi, lo)
			continue
		}
		fmt.Fprintf(&out, `\u%04x`, r)
	}
	return out.Bytes()
}
