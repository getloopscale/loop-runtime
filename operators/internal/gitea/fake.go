package gitea

import (
	"context"
	"fmt"
	"sync"
)

// FakeProvisioner is a test double for GitRepoProvisioner.
// It tracks created repos and can be configured to return errors.
type FakeProvisioner struct {
	mu    sync.Mutex
	repos map[string]string // name → URL
	Err   error             // if set, all calls return this error
	Calls int               // number of EnsureRepo calls
}

func NewFakeProvisioner() *FakeProvisioner {
	return &FakeProvisioner{
		repos: make(map[string]string),
	}
}

func (f *FakeProvisioner) EnsureRepo(_ context.Context, name string, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.Calls++

	if f.Err != nil {
		return "", f.Err
	}

	if url, ok := f.repos[name]; ok {
		return url, nil
	}

	url := fmt.Sprintf("http://gitea.loopzero-system.svc:3000/loopzero/%s.git", name)
	f.repos[name] = url
	return url, nil
}

func (f *FakeProvisioner) RepoExists(_ context.Context, name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.Err != nil {
		return false, f.Err
	}

	_, ok := f.repos[name]
	return ok, nil
}

func (f *FakeProvisioner) ArchiveRepo(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.Err != nil {
		return f.Err
	}

	delete(f.repos, name)
	return nil
}
