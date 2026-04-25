// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package manifests

import (
	"fmt"
	"strings"
)

// buildService creates a Service resource for an app.
func buildService(ri RenderInput, app AppManifestInput, port int) k8sService {
	ns := namespace(ri.TenantID, ri.Environment)
	labels := commonLabels(ri, app)

	return k8sService{
		APIVersion: "v1",
		Kind:       "Service",
		Metadata: k8sMetadata{
			Name:      app.Name,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: k8sServiceSpec{
			Selector: map[string]string{"app": app.Name},
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
func buildIngressResources(ri RenderInput, app AppManifestInput, port int) []string {
	ns := namespace(ri.TenantID, ri.Environment)
	labels := commonLabels(ri, app)
	hostname := appHostname(ri.TenantID, ri.ProjectID, app.Name, ri.Environment, ri.BaseDomain)
	tlsSecretName := fmt.Sprintf("tls-%s", app.Name)

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

	// When scale-to-zero is enabled, route traffic through the KEDA HTTP Add-on
	// interceptor proxy. The interceptor holds requests while scaling from 0→1,
	// then proxies to the real app service once the pod is ready.
	// When scale-to-zero is NOT enabled, route directly to the app service.
	serviceTarget := traefikRouteService{Name: app.Name, Port: port}
	if isScaleToZero(app.Autoscaling) {
		serviceTarget = traefikRouteService{
			Name:      "keda-add-ons-http-interceptor-proxy",
			Namespace: "keda",
			Port:      8080,
		}
	}

	ingress := traefikIngressRoute{
		APIVersion: "traefik.io/v1alpha1",
		Kind:       "IngressRoute",
		Metadata: k8sMetadata{
			Name:      app.Name,
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

	docs := []string{marshalResource(cert), marshalResource(ingress)}

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
						"loopzero.io/custom-domain": cd.Domain,
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
			docs = append(docs, marshalResource(cdCert))
		}

		// IngressRoute always needed — Traefik must match Host() for routing
		// Custom domain IngressRoute also routes through interceptor for scale-to-zero
		cdServiceTarget := traefikRouteService{Name: app.Name, Port: port}
		if isScaleToZero(app.Autoscaling) {
			cdServiceTarget = traefikRouteService{
				Name:      "keda-add-ons-http-interceptor-proxy",
				Namespace: "keda",
				Port:      8080,
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
					"loopzero.io/custom-domain": cd.Domain,
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

		docs = append(docs, marshalResource(cdIngress))
	}

	return docs
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
