// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package postgresrestore

import (
	"errors"
	"testing"
)

const backedUpCluster = `{
  "spec": {
    "imageName": "registry.example/postgresql:19.0",
    "storage": {"size": "5Gi", "storageClass": "gp3"},
    "plugins": [{"name": "barman-cloud.cloudnative-pg.io", "isWALArchiver": true,
                 "parameters": {"barmanObjectName": "platform-postgres", "serverName": "platform-postgres-r1"}}],
    "serviceAccountTemplate": {"metadata": {"annotations": {"eks.amazonaws.com/role-arn": "arn:aws:iam::1:role/pg"}}}
  },
  "status": {"currentPrimary": "platform-postgres-2"}
}`

func TestSourceIsReadFromTheClusterObject(t *testing.T) {
	src, err := sourceFromJSON("platform-postgres", "loop-runtime-data-plane", []byte(backedUpCluster))

	if err != nil {
		t.Fatal(err)
	}
	want := Source{
		Name: "platform-postgres", Namespace: "loop-runtime-data-plane",
		Image: "registry.example/postgresql:19.0", StorageSize: "5Gi", StorageClass: "gp3",
		ObjectStore: "platform-postgres", ServerName: "platform-postgres-r1",
		RoleARN: "arn:aws:iam::1:role/pg", Primary: "platform-postgres-2",
	}
	if src != want {
		t.Errorf("source = %+v\nwant     %+v", src, want)
	}
}

func TestSourceRefusesAClusterWithoutBackups(t *testing.T) {
	_, err := sourceFromJSON("platform-postgres", "ns",
		[]byte(`{"spec": {"imageName": "x"}, "status": {"currentPrimary": "platform-postgres-1"}}`))

	if !errors.Is(err, ErrNotBackedUp) {
		t.Errorf("err = %v, want ErrNotBackedUp", err)
	}
}
