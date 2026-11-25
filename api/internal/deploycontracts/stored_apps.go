// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package deploycontracts

import "github.com/getloopscale/loopzero/api/internal/db"

// AppSpecFromStored rebuilds the AppSpec a build was submitted with from the
// app spec that build persisted. It is the one mapping from the stored shape
// back to the submission shape, the counterpart of ConvertLoopZeroYamlApps for
// builds that start from a previous build rather than from loopzero.yaml (PR
// previews). Every persisted field is carried: dropping Runtime, Path, Build
// or Start rebuilds the app with the wrong strategy (loopzero#185).
func AppSpecFromStored(s db.BuildAppSpec) AppSpec {
	app := AppSpec{
		Name:      s.Name,
		Type:      s.Type,
		Rendering: s.Rendering,
		Port:      s.Port,
		Start:     s.Start,
		Path:      s.Path,
		Build:     s.Build,
		Runtime:   s.Runtime,
		Secrets:   s.Secrets,
		Replicas:  s.Replicas,
	}
	if s.Autoscaling != nil {
		app.Autoscaling = &AppAutoscalingSpec{
			MinReplicas:  s.Autoscaling.MinReplicas,
			MaxReplicas:  s.Autoscaling.MaxReplicas,
			TargetCPU:    s.Autoscaling.TargetCPU,
			TargetMemory: s.Autoscaling.TargetMemory,
		}
	}
	return app
}
