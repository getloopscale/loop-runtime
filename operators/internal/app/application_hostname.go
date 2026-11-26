// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package app

import (
	"errors"
	"fmt"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/pkg/naming"
)

// errApplicationNameUnknown is returned when a LoopRuntimeApplication carries neither
// spec.appName nor the runtime.getloopscale.com/app label and has no hostname yet: the routed
// hostname cannot be composed without the app name, and inventing one would
// publish a host no IngressRoute matches.
var errApplicationNameUnknown = errors.New("application name unknown: neither spec.appName nor the " + naming.LabelApp + " label is set")

// hostnameToAssign decides what the controller writes to spec.hostname. It
// returns assign=false when the stored value must be kept.
//
// The default is naming.AppHostname — the formula the IngressRoute renderer
// emits, so the ExposedRoute and status.url name a host Traefik routes. The
// controller used to default to "{project}-{env}.{base}" (naming.Hostname,
// since removed), which no IngressRoute matched (J-25). A stored hostname equal
// to that old default was written by the controller itself, not chosen by
// anyone, so it is replaced; any other stored hostname is an explicit choice
// and is kept.
func hostnameToAssign(app *loopruntimev1.LoopRuntimeApplication, baseDomain string) (hostname string, assign bool, err error) {
	stored := app.Spec.Hostname
	if stored != "" && stored != legacyDefaultHostname(app, baseDomain) {
		return "", false, nil
	}
	appName := applicationName(app)
	if appName == "" {
		if stored != "" {
			// A legacy default on a CR that names no app: nothing better can
			// be composed, and clearing it would take the app off the air.
			return "", false, nil
		}
		return "", false, fmt.Errorf("LoopRuntimeApplication %s/%s: %w", app.Namespace, app.Name, errApplicationNameUnknown)
	}
	return naming.AppHostname(app.Spec.TenantRef, app.Spec.Project, appName, app.Spec.Environment, baseDomain), true, nil
}

// applicationName is spec.appName, or — for CRs written before that field
// existed — the runtime.getloopscale.com/app label the api has always set.
func applicationName(app *loopruntimev1.LoopRuntimeApplication) string {
	if app.Spec.AppName != "" {
		return app.Spec.AppName
	}
	return app.Labels[naming.LabelApp]
}

// legacyDefaultHostname is the hostname the controller used to write when
// spec.hostname was empty: "{sanitized project-env}.{base}". It exists only so
// hostnameToAssign can recognise and replace that value.
func legacyDefaultHostname(app *loopruntimev1.LoopRuntimeApplication, baseDomain string) string {
	subdomain := naming.Sanitize(app.Spec.Project + "-" + app.Spec.Environment)
	if subdomain == "" {
		return ""
	}
	return subdomain + "." + baseDomain
}
