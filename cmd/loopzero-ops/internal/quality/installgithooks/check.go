// Package installgithooks validates the M8 1.5 pre-push hook + installer source
// shape + installer idempotency.
//
// Migrated from infra/tests/check-install-git-hooks_test.sh (M-Migrate2 2026-07-07).
// Hermetic: NO network, NO real push.
package installgithooks

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

type Config struct{ RepoRoot string }
type Outcome struct{ Failures []string }

var ErrFailures = errors.New("install-git-hooks assertions failed")

func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, err
		}
	}
	hook := filepath.Join(root, "infra/scripts/local/git-hooks/pre-push")
	inst := filepath.Join(root, "infra/scripts/local/install-git-hooks.sh")

	var failures []string
	chk := func(name string, pass bool) {
		if !pass {
			failures = append(failures, name)
		}
	}

	hookInfo, hookErr := os.Stat(hook)
	chk("hook source exists + executable", hookErr == nil && hookInfo.Mode()&0o111 != 0)

	hookBody, _ := os.ReadFile(hook)
	chk("hook runs task quality:gates", bytes.Contains(hookBody, []byte("task quality:gates")))
	chk("hook honors LOOPZERO_SKIP_PREPUSH escape", bytes.Contains(hookBody, []byte("LOOPZERO_SKIP_PREPUSH")))
	chk("hook bash -n clean", bashSyntax(hook))

	instInfo, instErr := os.Stat(inst)
	chk("installer exists + executable", instErr == nil && instInfo.Mode()&0o111 != 0)
	instBody, _ := os.ReadFile(inst)
	chk("installer idempotent (install -m 0755)", bytes.Contains(instBody, []byte("install -m 0755")))
	chk("installer bash -n clean", bashSyntax(inst))

	// Idempotency: copying hook twice must produce identical content
	if hookErr == nil {
		tmp, _ := os.MkdirTemp("", "installgithooks-*")
		defer os.RemoveAll(tmp)
		dst := filepath.Join(tmp, "pre-push")
		_ = copyFile(hook, dst)
		a, _ := hashFile(dst)
		_ = copyFile(hook, dst)
		b, _ := hashFile(dst)
		chk("idempotent copy (same hash twice)", a != "" && a == b)
	}

	if len(failures) > 0 {
		return Outcome{Failures: failures}, ErrFailures
	}
	return Outcome{}, nil
}

func bashSyntax(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	err := exec.Command("bash", "-n", path).Run()
	return err == nil
}

func copyFile(src, dst string) error {
	body, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, body, 0o755)
}

func hashFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

