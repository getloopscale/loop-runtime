package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// `[ -f Taskfile.yml ]` is a presence check, and the claim built on it was that the
// tree is IDENTIFIED. An audit falsified that on 2026-09-05 by building a directory
// holding one Taskfile.yml whose `quality:all` is `echo "I am not loopzero, and I checked
// nothing."`, pointing CLAUDE_PROJECT_DIR at it and pushing:
//
//	[delivery-gate] quality:all GREEN — push allowed.
//
// That is the family this item exists to close, arriving through the door the item
// added: the gate ran something, it was not this repository's suite, and the push was
// allowed on the strength of it.
//
// The check now asks for landmarks a Taskfile alone does not carry. It is still not
// proof of identity — nothing short of a checksum would be — and the header says so.
// What it is: enough that a stray Taskfile in some other tree cannot answer for this
// one, which is the failure that was measured.

func TestDeliveryGate_RefusesATreeThatMerelyHasATaskfile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Taskfile.yml"),
		[]byte("version: '3'\ntasks:\n  quality:all:\n    cmds:\n      - echo \"I am not loopzero, and I checked nothing.\"\n"),
		0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cmd := exec.Command("bash", deliveryGatePath(t))
	cmd.Stdin = strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"git push origin main"}}`)
	cmd.Env = append(os.Environ(), "CLAUDE_PROJECT_DIR="+dir)
	out, _ := cmd.CombinedOutput()

	if strings.Contains(string(out), "GREEN") {
		t.Fatalf("a directory holding one Taskfile answered for this repository:\n%s", out)
	}
	if !strings.Contains(string(out), "BLOCKED") {
		t.Fatalf("it must refuse and say why; got:\n%s", out)
	}
}

func TestDeliveryGate_NamesWhichLandmarkIsMissing(t *testing.T) {
	// A refusal that does not say what it looked for sends the reader to the source.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Taskfile.yml"), []byte("version: '3'\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cmd := exec.Command("bash", deliveryGatePath(t))
	cmd.Stdin = strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"git push origin main"}}`)
	cmd.Env = append(os.Environ(), "CLAUDE_PROJECT_DIR="+dir)
	out, _ := cmd.CombinedOutput()

	if !strings.Contains(string(out), "taskfiles/quality.yml") &&
		!strings.Contains(string(out), "cmd/loopzero-ops") {
		t.Fatalf("the refusal must name the landmark it did not find; got:\n%s", out)
	}
}
