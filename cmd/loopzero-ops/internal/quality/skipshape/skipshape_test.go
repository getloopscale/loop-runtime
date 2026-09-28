// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package skipshape

import (
	"go/parser"
	"testing"
)

func TestIsSkipCall(t *testing.T) {
	cases := []struct {
		expr string
		want bool
	}{
		{`t.Skip("x")`, true},
		{`t.Skipf("%s", x)`, true},
		{`t.SkipNow()`, true},
		{`h.t.Skip("reached through a field")`, true},
		{`s.T().Skip()`, true},
		{`t.Fatal("x")`, false},
		{`Skip()`, false},
		{`t.Skip`, false},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			e, err := parser.ParseExpr(c.expr)
			if err != nil {
				t.Fatalf("parse %q: %v", c.expr, err)
			}

			if got := IsSkipCall(e); got != c.want {
				t.Errorf("IsSkipCall(%s) = %v, want %v", c.expr, got, c.want)
			}
		})
	}
}
