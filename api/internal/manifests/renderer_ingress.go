// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package manifests

import (
	"fmt"

	"strings"

	"github.com/getloopscale/loop-runtime/pkg/naming"
)

// buildService creates a Service resource for an app.
func buildService(ri renderScope, app AppManifestInput, port int) k8sService {
	ns := ri.ns
	labels := commonLabels(ri, app)
	resName := resourceName(ri.ProjectID, app.Name)

	return k8sService{
		APIVersion: "v1",
		Kind:       "Service",
		Metadata: k8sMetadata{
			Name:      resName,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: k8sServiceSpec{
			// Selector matches the project-scoped `app` label so pods of
			// another project (e.g., a different `api`) are never picked
			// up. Achado #17 fix.
			Selector: map[string]string{"app": resName},
			Ports: []k8sServicePort{
				{Port: port, TargetPort: port, Protocol: "TCP"},
			},
		},
	}
}

// buildIngressResources generates a Certificate CR and IngressRoute for TLS.
// cert-manager issues a Let's Encrypt certificate via the ClusterIssuer.
// The IngressRoute references the certificate secret for TLS termination.
// Hostname format: {app}-{env}.{project}.{tenant}.{baseDomain}
func buildIngressResources(ri renderScope, app AppManifestInput, port int) ([]string, error) {
	ns := ri.ns
	labels := commonLabels(ri, app)
	resName := resourceName(ri.ProjectID, app.Name)
	hostname := appHostname(ri.TenantID, ri.ProjectID, app.Name, ri.Environment, ri.BaseDomain)
	// Cert + IngressRoute names use the project-scoped resource name so two
	// projects with same app name don't share a cert secret (Achado #17).
	//
	// #277: the name comes from naming.AppCertSecretName, not from a local Sprintf. The
	// exposure controller must look up exactly this Secret, and deriving the same name with
	// two different formulas is what kept every ExposedRoute Pending for 27 days.
	tlsSecretName, certErr := naming.AppCertSecretName(ri.ProjectID, app.Name)
	if certErr != nil {
		// Fail loud: an invalid name here would produce a Certificate the controller
		// never finds — exactly the silent failure mode of #277.
		return nil, fmt.Errorf("render: cert secret name for %s/%s: %w", ri.ProjectID, app.Name, certErr)
	}

	cert := certManagerCertificate{
		APIVersion: "cert-manager.io/v1",
		Kind:       "Certificate",
		Metadata: k8sMetadata{
			Name:      tlsSecretName,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: certManagerCertificateSpec{
			SecretName: tlsSecretName,
			IssuerRef: certIssuerRef{
				Name: ri.clusterIssuerName(),
				Kind: "ClusterIssuer",
			},
			DNSNames: []string{hostname},
		},
	}

	// Routing target priority:
	//   1. KEDA interceptor when scale-to-zero is enabled (cold-start handling).
	//   2. TraefikService (weighted) when progressive delivery is enabled —
	//      argo-rollouts-progressive-delivery-plan T1.4 / EC-2: route to
	//      weighted TraefikService so Argo Rollouts can shift traffic between
	//      stable and canary services during canary steps.
	//   3. Direct to Service for legacy / standard apps.
	serviceTarget := traefikRouteService{Name: resName, Port: port}
	if isScaleToZero(app.Autoscaling) {
		serviceTarget = traefikRouteService{
			Name:      "keda-add-ons-http-interceptor-proxy",
			Namespace: "keda",
			Port:      8080,
		}
	} else if ri.ProgressiveDelivery {
		// EC-2 fix: Kind=TraefikService routes to the weighted CRD; the
		// TraefikService itself defines port/services internally.
		serviceTarget = traefikRouteService{
			Name: resName,
			Kind: "TraefikService",
		}
	}

	ingress := traefikIngressRoute{
		APIVersion: "traefik.io/v1alpha1",
		Kind:       "IngressRoute",
		Metadata: k8sMetadata{
			Name:      resName,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: traefikIngressRouteSpec{
			EntryPoints: []string{"web", "websecure"},
			Routes: []traefikRoute{
				{
					Match:    fmt.Sprintf("Host(`%s`)", hostname),
					Kind:     "Rule",
					Services: []traefikRouteService{serviceTarget},
				},
			},
			TLS: &traefikTLS{SecretName: tlsSecretName},
		},
	}

	var docs docSet
	docs.add(cert)
	docs.add(ingress)

	// Custom domains: each gets its own IngressRoute + Certificate (MEET-2026-04-05-033).
	// Resource names are domain-derived (not index-based) to prevent churn on add/remove.
	for _, cd := range app.CustomDomains {
		suffix, cdTLSName := domainResourceNames(cd.Domain)

		// v2 (CF-managed): CF handles TLS at the edge — no cert-manager Certificate needed.
		// Only generate IngressRoute for Traefik routing (CF Tunnel → Traefik → Pod).
		// v1 (non-CF): Generate both Certificate + IngressRoute (cert-manager handles TLS).
		if !cd.CFManaged {
			cdCert := certManagerCertificate{
				APIVersion: "cert-manager.io/v1",
				Kind:       "Certificate",
				Metadata: k8sMetadata{
					Name:      cdTLSName,
					Namespace: ns,
					Labels:    labels,
					Annotations: map[string]string{
						"runtime.getloopscale.com/custom-domain": cd.Domain,
					},
				},
				Spec: certManagerCertificateSpec{
					SecretName: cdTLSName,
					IssuerRef: certIssuerRef{
						Name: ri.clusterIssuerName(),
						Kind: "ClusterIssuer",
					},
					DNSNames: []string{cd.Domain},
				},
			}
			docs.add(cdCert)
		}

		// IngressRoute always needed — Traefik must match Host() for routing.
		// Same target priority as primary route: scale-to-zero interceptor >
		// TraefikService (progressive delivery) > direct Service. EC-9 fix:
		// custom domains MUST also point at TraefikService when progressive
		// delivery is enabled, otherwise canary traffic split is bypassed.
		cdServiceTarget := traefikRouteService{Name: resName, Port: port}
		if isScaleToZero(app.Autoscaling) {
			cdServiceTarget = traefikRouteService{
				Name:      "keda-add-ons-http-interceptor-proxy",
				Namespace: "keda",
				Port:      8080,
			}
		} else if ri.ProgressiveDelivery {
			cdServiceTarget = traefikRouteService{
				Name: resName,
				Kind: "TraefikService",
			}
		}

		cdIngress := traefikIngressRoute{
			APIVersion: "traefik.io/v1alpha1",
			Kind:       "IngressRoute",
			Metadata: k8sMetadata{
				Name:      suffix,
				Namespace: ns,
				Labels:    labels,
				Annotations: map[string]string{
					"runtime.getloopscale.com/custom-domain": cd.Domain,
				},
			},
			Spec: traefikIngressRouteSpec{
				EntryPoints: []string{"web", "websecure"},
				Routes: []traefikRoute{
					{
						Match:    fmt.Sprintf("Host(`%s`)", cd.Domain),
						Kind:     "Rule",
						Services: []traefikRouteService{cdServiceTarget},
					},
				},
			},
		}

		// v1: TLS terminates at Traefik using cert-manager secret
		// v2: TLS terminates at CF edge — Traefik sees plain HTTP from tunnel
		if !cd.CFManaged {
			cdIngress.Spec.TLS = &traefikTLS{SecretName: cdTLSName}
		}

		docs.add(cdIngress)
	}

	return docs.result()
}

// domainResourceNames returns stable K8s resource names for a custom domain's
// IngressRoute and Certificate. Both names are guaranteed ≤ 63 characters.
// MEET-2026-04-05-033: Domain-derived naming prevents resource churn.
// CODE-005 fix: tls- prefix is accounted for in the length budget.
func domainResourceNames(domain string) (ingressName, certName string) {
	s := strings.ToLower(domain)
	s = strings.ReplaceAll(s, ".", "-")
	base := "cd-" + s

	// Budget: certName = "tls-" + ingressName, so ingressName max = 59 chars
	const maxIngressLen = 59

	if len(base) <= maxIngressLen {
		ingressName = base
	} else {
		// Truncate + append 8-char hash of full domain to prevent collisions
		h := fmt.Sprintf("%x", hashDomain(domain))[:8]
		ingressName = base[:maxIngressLen-9] + "-" + h // 50 + 1 + 8 = 59
	}
	ingressName = strings.TrimRight(ingressName, "-")

	certName = "tls-" + ingressName
	return ingressName, certName
}

// hashDomain returns a deterministic hash of a domain string for collision avoidance.
func hashDomain(domain string) uint64 {
	// FNV-1a 64-bit: fast, deterministic, no crypto needed (not a security hash).
	var h uint64 = 14695981039346656037
	for i := 0; i < len(domain); i++ {
		h ^= uint64(domain[i])
		h *= 1099511628211
	}
	return h
}
