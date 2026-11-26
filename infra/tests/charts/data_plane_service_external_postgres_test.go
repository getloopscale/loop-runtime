// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"strings"
	"testing"
)

// A data-plane service can use a database it does not run: on staging the platform's
// CloudNativePG cluster (infra/helm/platform-postgres) owns the database and the role.
// With postgres.external set the chart renders no PostgreSQL of its own and points the
// service at the host it was given.

const externalPostgresValues = `
image:
  repository: registry.example/looptrace
  tag: "0123456789abcdef0123456789abcdef01234567"
port: 4318
healthPath: /v1/healthz
existingSecret: looptrace
env:
  LOOPTRACE_PORT: "4318"
postgres:
  image: registry.example/postgres:16
  database: looptrace
  user: looptrace
  uriEnv: LOOPTRACE_PG_URI
  dataPath: /var/lib/postgresql/data
  storage: 1Gi
  external:
    host: platform-postgres-rw.loopruntime-data-plane.svc.cluster.local
    port: 5432
`

func TestDataPlaneService_ExternalPostgresRendersNoDatabase(t *testing.T) {
	docs := renderDataPlane(t, externalPostgresValues)
	for _, d := range docs {
		if d.obj.Kind == "StatefulSet" {
			t.Errorf("rendered StatefulSet %q: with postgres.external the chart must not run a PostgreSQL", d.obj.Metadata.Name)
		}
		if d.obj.Kind == "Service" && strings.HasSuffix(d.obj.Metadata.Name, "-postgres") {
			t.Errorf("rendered Service %q for a PostgreSQL the chart does not run", d.obj.Metadata.Name)
		}
	}
}

func TestDataPlaneService_ExternalPostgresPointsTheServiceAtTheHost(t *testing.T) {
	docs := renderDataPlane(t, externalPostgresValues)
	w := asWorkload(t, findDoc(t, docs, "Deployment", "loopruntime-memory"))

	const want = "postgres://looptrace:$(POSTGRES_PASSWORD)@platform-postgres-rw.loopruntime-data-plane.svc.cluster.local:5432/looptrace"
	var got string
	for _, e := range w.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "LOOPTRACE_PG_URI" {
			got = e.Value
		}
	}
	if got != want {
		t.Errorf("LOOPTRACE_PG_URI = %q, want %q", got, want)
	}

	wait := w.Spec.Template.Spec.InitContainers[0]
	if cmd := strings.Join(wait.Command, " "); !strings.Contains(cmd, "-h platform-postgres-rw.loopruntime-data-plane.svc.cluster.local -p 5432") {
		t.Errorf("wait-for-postgres waits on the wrong address: %s", cmd)
	}
}

func TestDataPlaneService_ExternalPostgresHonoursThePort(t *testing.T) {
	docs := renderDataPlane(t, strings.Replace(externalPostgresValues, "port: 5432", "port: 6432", 1))
	w := asWorkload(t, findDoc(t, docs, "Deployment", "loopruntime-memory"))
	for _, e := range w.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "LOOPTRACE_PG_URI" && !strings.Contains(e.Value, ":6432/looptrace") {
			t.Errorf("LOOPTRACE_PG_URI ignores postgres.external.port: %q", e.Value)
		}
	}
}

func TestDataPlaneService_WithoutExternalPostgresStillRunsItsOwn(t *testing.T) {
	docs := renderDataPlane(t, dataPlaneValues)
	findDoc(t, docs, "StatefulSet", "loopruntime-memory-postgres")
	findDoc(t, docs, "Service", "loopruntime-memory-postgres")
}

// On an installation whose secrets live in a secret store, the chart asks External Secrets
// for the Secret it reads, and for the basic-auth Secret the platform's PostgreSQL reads
// the role's password from. Both come from the same property, so the role and the service
// cannot disagree about the password.

const externalSecretValues = externalPostgresValues + `
externalSecret:
  store: aws-secrets-manager
  remoteKey: loopruntime/staging/platform
  keys:
    postgres-password: looptrace-db-password
    admin-bootstrap-token: looptrace-admin-bootstrap-token
  postgresRoleSecret: looptrace-postgres-role
`

func TestDataPlaneService_ExternalSecretWritesTheServiceSecret(t *testing.T) {
	docs := renderDataPlane(t, externalSecretValues)
	d := findDoc(t, docs, "ExternalSecret", "looptrace")
	for _, want := range []string{
		"name: aws-secrets-manager",
		"key: loopruntime/staging/platform",
		"secretKey: postgres-password",
		"property: looptrace-db-password",
		"secretKey: admin-bootstrap-token",
		"property: looptrace-admin-bootstrap-token",
	} {
		if !strings.Contains(d.raw, want) {
			t.Errorf("ExternalSecret looptrace lacks %q:\n%s", want, d.raw)
		}
	}
}

func TestDataPlaneService_ExternalSecretWritesThePostgresRoleSecret(t *testing.T) {
	docs := renderDataPlane(t, externalSecretValues)
	d := findDoc(t, docs, "ExternalSecret", "looptrace-postgres-role")
	for _, want := range []string{
		"type: kubernetes.io/basic-auth",
		"username: \"looptrace\"",
		"property: looptrace-db-password",
	} {
		if !strings.Contains(d.raw, want) {
			t.Errorf("ExternalSecret looptrace-postgres-role lacks %q:\n%s", want, d.raw)
		}
	}
}

func TestDataPlaneService_NoExternalSecretByDefault(t *testing.T) {
	for _, d := range renderDataPlane(t, dataPlaneValues) {
		if d.obj.Kind == "ExternalSecret" {
			t.Errorf("rendered ExternalSecret %q without externalSecret.store", d.obj.Metadata.Name)
		}
	}
}

// A private image needs a registry login in the service's own namespace. The chart writes
// it from the store and every pod of the service names it.
func TestDataPlaneService_ImagePullSecretComesFromTheStore(t *testing.T) {
	docs := renderDataPlane(t, externalSecretValues+`  imagePull:
    secretName: ghcr-pull
    remoteKey: loopruntime/staging/ghcr-pull
    registry: ghcr.io
`)
	d := findDoc(t, docs, "ExternalSecret", "ghcr-pull")
	for _, want := range []string{"type: kubernetes.io/dockerconfigjson", "loopruntime/staging/ghcr-pull", "ghcr.io"} {
		if !strings.Contains(d.raw, want) {
			t.Errorf("ExternalSecret ghcr-pull lacks %q:\n%s", want, d.raw)
		}
	}
	dep := findDoc(t, docs, "Deployment", "loopruntime-memory")
	if !strings.Contains(dep.raw, "imagePullSecrets:\n        - name: ghcr-pull") {
		t.Errorf("the Deployment does not pull with ghcr-pull:\n%s", dep.raw)
	}
}

func TestDataPlaneService_NoImagePullSecretByDefault(t *testing.T) {
	dep := findDoc(t, renderDataPlane(t, externalSecretValues), "Deployment", "loopruntime-memory")
	if strings.Contains(dep.raw, "imagePullSecrets") {
		t.Errorf("the Deployment names a pull secret nothing writes:\n%s", dep.raw)
	}
}

// The platform's PostgreSQL refuses a plaintext connection, so a service that uses it
// names the Secret holding the CA that signed its certificate: the chart mounts it and
// asks the driver to verify the certificate and the host name against it.
var externalPostgresTLSValues = strings.Replace(externalSecretValues,
	"    port: 5432\n", "    port: 5432\n    caSecret: platform-postgres-ca\n", 1)

func TestDataPlaneService_ExternalPostgresVerifiesTheServerAgainstItsCA(t *testing.T) {
	docs := renderDataPlane(t, externalPostgresTLSValues)
	dep := findDoc(t, docs, "Deployment", "loopruntime-memory")
	w := asWorkload(t, dep)

	var uri string
	for _, e := range w.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "LOOPTRACE_PG_URI" {
			uri = e.Value
		}
	}
	if !strings.HasSuffix(uri, "/looptrace?sslmode=verify-full&sslrootcert=/etc/postgres-ca/ca.crt") {
		t.Errorf("LOOPTRACE_PG_URI does not verify the server: %q", uri)
	}
	for _, want := range []string{
		"secretName: platform-postgres-ca",
		"mountPath: /etc/postgres-ca",
	} {
		if !strings.Contains(dep.raw, want) {
			t.Errorf("the Deployment lacks %q", want)
		}
	}
}

func TestDataPlaneService_ExternalPostgresWithoutACAKeepsThePlainURI(t *testing.T) {
	dep := findDoc(t, renderDataPlane(t, externalSecretValues), "Deployment", "loopruntime-memory")
	if strings.Contains(dep.raw, "sslmode") || strings.Contains(dep.raw, "postgres-ca") {
		t.Errorf("TLS settings rendered with no postgres.external.caSecret:\n%s", dep.raw)
	}
}

// The operator applies a changed password at once only to a Secret carrying this label;
// without it the role keeps the old password until the operator's cache is refreshed.
func TestDataPlaneService_PostgresRoleSecretAsksTheOperatorToReload(t *testing.T) {
	d := findDoc(t, renderDataPlane(t, externalSecretValues), "ExternalSecret", "looptrace-postgres-role")
	if !strings.Contains(d.raw, `cnpg.io/reload: "true"`) {
		t.Errorf("the role Secret lacks the cnpg.io/reload label:\n%s", d.raw)
	}
}

// A Service named after the product makes Kubernetes inject <NAME>_PORT=tcp://... into
// every pod of the namespace, which is the variable the product reads its port from.
func TestDataPlaneService_PodsTakeNoServiceLinks(t *testing.T) {
	dep := findDoc(t, renderDataPlane(t, dataPlaneValues), "Deployment", "loopruntime-memory")
	if !strings.Contains(dep.raw, "enableServiceLinks: false") {
		t.Errorf("the pod takes service links:\n%s", dep.raw)
	}
}
