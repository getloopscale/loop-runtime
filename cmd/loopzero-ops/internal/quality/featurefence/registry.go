// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package featurefence

// registry is the deny-by-default map of api/internal. Every top-level directory
// holding Go code must be here, so a new one is a decision rather than a
// discovery.
var registry = map[string]Entry{
	// ── features ────────────────────────────────────────────────────────────
	"audit":     {Kind: Feature},
	"metering":  {Kind: Feature, Deps: []string{"placement"}},
	"placement": {Kind: Feature},

	// ── shared kernel ───────────────────────────────────────────────────────
	"archive":         {Kind: Kernel},
	"config":          {Kind: Kernel},
	"correlation":     {Kind: Kernel},
	"db":              {Kind: Kernel, Deps: []string{"metrics", "ports", "projectstatus"}},
	"deploycontracts": {Kind: Kernel, Deps: []string{"db", "manifests", "ports"}},
	"events":          {Kind: Kernel},
	"gitclient":       {Kind: Kernel},
	"github":          {Kind: Kernel, Deps: []string{"archive", "gitclient", "metrics", "ports"}},
	"gitops":          {Kind: Kernel, Deps: []string{"deploycontracts", "gitclient", "metrics"}},
	"k8s":             {Kind: Kernel, Deps: []string{"metrics", "ports"}},
	"leader":          {Kind: Kernel, Deps: []string{"db"}},
	"lock":            {Kind: Kernel, Deps: []string{"metrics"}},
	"manifests":       {Kind: Kernel},
	"metrics":         {Kind: Kernel, Deps: []string{"surfaces"}},
	"middleware":      {Kind: Kernel, Deps: []string{"metrics", "ports"}},
	"objectstore":     {Kind: Kernel},
	"ports":           {Kind: Kernel},
	"processing":      {Kind: Kernel, Deps: []string{"db"}},
	"projectstatus":   {Kind: Kernel},
	"scrub":           {Kind: Kernel},
	"sse":             {Kind: Kernel, Deps: []string{"db", "metrics", "middleware", "ports"}},
	"surfaces":        {Kind: Kernel},
	"tracing":         {Kind: Kernel},
	"validation":      {Kind: Kernel},

	// ── layer folders being dissolved into features ─────────────────────────
	// Each entry below disappears when its code reaches the feature that owns it.
	"adapters":   {Kind: Layer, Deps: []string{"aggregator", "buildplane", "cfzone", "db", "deploycontracts", "github", "k8s", "lock", "ports", "routes", "services", "sse"}},
	"aggregator": {Kind: Layer, Deps: []string{"metrics", "ports", "scrub"}},
	"argocd":     {Kind: Layer, Deps: []string{"deploycontracts", "k8s", "metrics", "placement"}},
	"buildplane": {Kind: Layer, Deps: []string{"archive", "deploycontracts", "events", "gitclient", "k8s", "metrics", "scrub"}},
	"cfkv":       {Kind: Layer, Deps: []string{"metrics"}},
	"cfzone":     {Kind: Layer},
	"loki":       {Kind: Layer, Deps: []string{"ports"}},
	"routes":     {Kind: Layer, Deps: []string{"buildplane", "correlation", "db", "github", "k8s", "metrics", "middleware", "ports", "services", "sse", "validation"}},
	"services":   {Kind: Layer, Deps: []string{"cfkv", "db", "deploycontracts", "events", "manifests", "metrics", "placement", "ports", "processing", "scrub", "sse"}},

	// ── composition roots ───────────────────────────────────────────────────
	"server":          {Kind: Composition},
	"backgroundloops": {Kind: Composition},

	// ── tests about the tree ────────────────────────────────────────────────
	"archtest": {Kind: TestOnly},
	"contract": {Kind: TestOnly, Deps: []string{"routes"}},
}
