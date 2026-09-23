// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package shellrun

// Option configures a Runner invocation (functional options).
type Option func(*runOptions)

// runOptions holds the per-call knobs. Only the ones a caller uses exist:
// options with no caller are not kept "for later" (YAGNI).
type runOptions struct {
	cwd string
}

// WithCwd sets the working directory for the child process. Default
// is the parent's cwd. Absolute paths recommended for reproducibility.
func WithCwd(dir string) Option {
	return func(o *runOptions) {
		o.cwd = dir
	}
}
