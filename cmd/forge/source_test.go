package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const testRevision = "0123456789abcdef0123456789abcdef01234567"

type fakeGitRunner struct {
	calls      [][]string
	revision   string
	cloneError error
}

func (f *fakeGitRunner) Run(
	_ context.Context,
	_ io.Writer,
	_ io.Writer,
	args ...string,
) error {
	f.record(args)
	if f.cloneError != nil {
		return f.cloneError
	}
	checkoutPath := args[len(args)-1]
	if err := os.MkdirAll(checkoutPath, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(checkoutPath, "Dockerfile"), []byte("FROM scratch\n"), 0o600)
}

func (f *fakeGitRunner) Output(_ context.Context, args ...string) (string, error) {
	f.record(args)
	return f.revision + "\n", nil
}

func (f *fakeGitRunner) record(args []string) {
	f.calls = append(f.calls, append([]string(nil), args...))
}

type fakeDeploymentSourceResolver struct {
	source deploymentSource
	err    error
}

func (f fakeDeploymentSourceResolver) Resolve(_ context.Context, _, _ string) (deploymentSource, error) {
	return f.source, f.err
}

func TestFileDeploymentSourceResolver(t *testing.T) {
	t.Run("keeps local paths local", func(t *testing.T) {
		git := &fakeGitRunner{revision: testRevision}
		resolver := fileDeploymentSourceResolver{git: git, managedRoot: t.TempDir()}

		source, err := resolver.Resolve(context.Background(), "./example", "")
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if source != (deploymentSource{Path: "example"}) {
			t.Errorf("source = %#v, want local path", source)
		}
		if len(git.calls) != 0 {
			t.Errorf("Git calls = %#v, want none", git.calls)
		}
	})

	t.Run("clones and pins a remote repository", func(t *testing.T) {
		const repositoryURL = "https://github.com/example/service.git"
		managedRoot := t.TempDir()
		git := &fakeGitRunner{revision: testRevision}
		resolver := fileDeploymentSourceResolver{git: git, managedRoot: managedRoot}

		source, err := resolver.Resolve(context.Background(), repositoryURL, "")
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if source.Origin != repositoryURL || source.Revision != testRevision {
			t.Errorf("source = %#v, want origin and revision", source)
		}
		if filepath.Base(source.Path) != "service" || filepath.Base(filepath.Dir(source.Path)) != testRevision {
			t.Errorf("checkout path = %q, want revision-pinned service directory", source.Path)
		}
		if _, err := os.Stat(filepath.Join(source.Path, "Dockerfile")); err != nil {
			t.Fatalf("inspect managed checkout: %v", err)
		}

		if len(git.calls) != 2 {
			t.Fatalf("Git calls = %#v, want clone and rev-parse", git.calls)
		}
		wantClonePrefix := []string{"clone", "--depth", "1", "--no-tags", "--", repositoryURL}
		if !reflect.DeepEqual(git.calls[0][:len(wantClonePrefix)], wantClonePrefix) {
			t.Errorf("clone call = %#v, want prefix %#v", git.calls[0], wantClonePrefix)
		}
		wantRevisionCall := []string{"-C", git.calls[0][len(git.calls[0])-1], "rev-parse", "HEAD"}
		if !reflect.DeepEqual(git.calls[1], wantRevisionCall) {
			t.Errorf("revision call = %#v, want %#v", git.calls[1], wantRevisionCall)
		}
	})

	t.Run("uses a requested workspace", func(t *testing.T) {
		const repositoryURL = "https://github.com/example/custom.git"
		requestedRoot := filepath.Join(t.TempDir(), "custom-workspace")
		resolver := fileDeploymentSourceResolver{
			git:         &fakeGitRunner{revision: testRevision},
			managedRoot: filepath.Join(t.TempDir(), "ignored-workspace"),
		}

		source, err := resolver.Resolve(context.Background(), repositoryURL, requestedRoot)
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if !strings.HasPrefix(source.Path, requestedRoot+string(filepath.Separator)) {
			t.Errorf("checkout path = %q, want it under %q", source.Path, requestedRoot)
		}
	})

	t.Run("cleans a failed clone", func(t *testing.T) {
		managedRoot := t.TempDir()
		resolver := fileDeploymentSourceResolver{
			git:         &fakeGitRunner{cloneError: errors.New("authentication failed")},
			managedRoot: managedRoot,
		}

		_, err := resolver.Resolve(context.Background(), "https://github.com/example/private.git", "")
		if err == nil || !strings.Contains(err.Error(), "clone repository") {
			t.Fatalf("error = %v, want clone error", err)
		}
		matches, globErr := filepath.Glob(filepath.Join(managedRoot, "repositories", "*", "clone-*"))
		if globErr != nil {
			t.Fatalf("glob temporary checkouts: %v", globErr)
		}
		if len(matches) != 0 {
			t.Errorf("temporary checkouts = %#v, want none", matches)
		}
	})
}

func TestFileDeploymentPlanResolverCarriesSourceMetadata(t *testing.T) {
	applicationPath := filepath.Join(t.TempDir(), "remote-service")
	mustMakeDir(t, applicationPath)
	mustWriteFile(t, filepath.Join(applicationPath, "Dockerfile"), "FROM scratch\n")

	resolver := fileDeploymentPlanResolver{sources: fakeDeploymentSourceResolver{source: deploymentSource{
		Path:     applicationPath,
		Origin:   "https://github.com/example/service.git",
		Revision: testRevision,
	}}}
	plan, err := resolver.Resolve(context.Background(), "ignored input", ".forge")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if plan.Source != "https://github.com/example/service.git" || plan.Revision != testRevision {
		t.Errorf("plan = %#v, want source metadata", plan)
	}
}

func TestGitSourceRecognition(t *testing.T) {
	for _, source := range []string{
		"https://github.com/example/service.git",
		"ssh://git@github.com/example/service.git",
		"git@github.com:example/service.git",
	} {
		if !isRemoteGitSource(source) {
			t.Errorf("isRemoteGitSource(%q) = false, want true", source)
		}
	}
	if isRemoteGitSource("../local-service") {
		t.Errorf("local path detected as remote")
	}

	got := publicGitOrigin("https://token:secret@github.com/example/service.git?access_token=secret#main")
	if got != "https://github.com/example/service.git" {
		t.Errorf("publicGitOrigin() = %q", got)
	}
}
