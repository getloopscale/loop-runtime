// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package release

import "context"

// ArgoAppLabelReader reads the labels off an ArgoCD Application CR by name.
//
// SyncCallbackActivator resolves the Application's tenant and environment from
// the runtime.getloopscale.com/tenant and runtime.getloopscale.com/environment labels the tenant operator sets.
// It is the only decoder: the Application name is naming.ArgoAppName's
// "{tenant}-{project}-{env}", every segment of which may contain hyphens, so no
// positional split of the name inverts it. (A LastIndex("-") parser of the
// pre-#271 "{tenant}-{env}" name ran as a fallback until 2026-09; it answered a
// wrong tenant for every current name and was removed.)
// The Kubernetes implementation is adapters.DynamicArgoAppLabelReader.
type ArgoAppLabelReader interface {
	GetAppLabels(ctx context.Context, appName string) (map[string]string, error)
}
