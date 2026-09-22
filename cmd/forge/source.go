package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var gitRevisionPattern = regexp.MustCompile(`^[0-9a-fA-F]{40,64}$`)

type deploymentSource struct {
	Path     string
	Origin   string
	Revision string
}

type deploymentSourceResolver interface {
	Resolve(ctx context.Context, input, managedRoot string) (deploymentSource, error)
}

type gitRunner interface {
	Run(ctx context.Context, stdout, stderr io.Writer, args ...string) error
	Output(ctx context.Context, args ...string) (string, error)
}

type execGitRunner struct{}

func (execGitRunner) Run(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	args ...string,
) error {
	command := exec.CommandContext(ctx, "git", args...)
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("run git %q: %w", args, err)
	}
	return nil
}

func (execGitRunner) Output(ctx context.Context, args ...string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := exec.CommandContext(ctx, "git", args...)
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return "", fmt.Errorf("run git %q: %s: %w", args, detail, err)
		}
		return "", fmt.Errorf("run git %q: %w", args, err)
	}
	return stdout.String(), nil
}

type fileDeploymentSourceResolver struct {
	git         gitRunner
	managedRoot string
}

func (r fileDeploymentSourceResolver) Resolve(
	ctx context.Context,
	input string,
	requestedRoot string,
) (deploymentSource, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return deploymentSource{}, fmt.Errorf("deployment source must not be empty")
	}
	if !isRemoteGitSource(input) {
		return deploymentSource{Path: filepath.Clean(input)}, nil
	}

	managedRoot := strings.TrimSpace(requestedRoot)
	if managedRoot == "" {
		managedRoot = r.managedRoot
	}
	if managedRoot == "" {
		workingDirectory, err := os.Getwd()
		if err != nil {
			return deploymentSource{}, fmt.Errorf("locate Forge working directory: %w", err)
		}
		managedRoot = filepath.Join(workingDirectory, ".forge")
	}
	managedRoot, err := filepath.Abs(managedRoot)
	if err != nil {
		return deploymentSource{}, fmt.Errorf("resolve Forge workspace %s: %w", managedRoot, err)
	}

	repositoryKey := fmt.Sprintf("%x", sha256.Sum256([]byte(input)))[:16]
	repositoryRoot := filepath.Join(managedRoot, "repositories", repositoryKey)
	if err := os.MkdirAll(repositoryRoot, 0o700); err != nil {
		return deploymentSource{}, fmt.Errorf("create managed repository directory: %w", err)
	}

	temporaryRoot, err := os.MkdirTemp(repositoryRoot, "clone-")
	if err != nil {
		return deploymentSource{}, fmt.Errorf("create temporary repository directory: %w", err)
	}
	defer os.RemoveAll(temporaryRoot)

	checkoutPath := filepath.Join(temporaryRoot, "repository")
	var cloneStderr bytes.Buffer
	if err := r.git.Run(
		ctx,
		io.Discard,
		&cloneStderr,
		"clone",
		"--depth", "1",
		"--no-tags",
		"--",
		input,
		checkoutPath,
	); err != nil {
		detail := strings.TrimSpace(cloneStderr.String())
		if detail != "" {
			return deploymentSource{}, fmt.Errorf("clone repository %s: %s: %w", publicGitOrigin(input), detail, err)
		}
		return deploymentSource{}, fmt.Errorf("clone repository %s: %w", publicGitOrigin(input), err)
	}

	revisionOutput, err := r.git.Output(ctx, "-C", checkoutPath, "rev-parse", "HEAD")
	if err != nil {
		return deploymentSource{}, fmt.Errorf("resolve repository revision: %w", err)
	}
	revision := strings.TrimSpace(revisionOutput)
	if !gitRevisionPattern.MatchString(revision) {
		return deploymentSource{}, fmt.Errorf("git returned invalid revision %q", revision)
	}

	repositoryName, err := repositoryNameFromGitSource(input)
	if err != nil {
		return deploymentSource{}, err
	}
	revisionRoot := filepath.Join(repositoryRoot, strings.ToLower(revision))
	finalPath := filepath.Join(revisionRoot, repositoryName)
	if info, err := os.Stat(finalPath); err == nil {
		if !info.IsDir() {
			return deploymentSource{}, fmt.Errorf("managed checkout path %s is not a directory", finalPath)
		}
		return deploymentSource{
			Path:     finalPath,
			Origin:   publicGitOrigin(input),
			Revision: strings.ToLower(revision),
		}, nil
	} else if !os.IsNotExist(err) {
		return deploymentSource{}, fmt.Errorf("inspect managed checkout %s: %w", finalPath, err)
	}

	if err := os.MkdirAll(revisionRoot, 0o700); err != nil {
		return deploymentSource{}, fmt.Errorf("create revision directory: %w", err)
	}
	if err := os.Rename(checkoutPath, finalPath); err != nil {
		return deploymentSource{}, fmt.Errorf("store managed checkout: %w", err)
	}
	return deploymentSource{
		Path:     finalPath,
		Origin:   publicGitOrigin(input),
		Revision: strings.ToLower(revision),
	}, nil
}

func repositoryNameFromGitSource(input string) (string, error) {
	path := input
	if parsed, err := url.Parse(input); err == nil && parsed.Host != "" {
		path = parsed.Path
	} else if separator := strings.Index(input, ":"); separator >= 0 {
		path = input[separator+1:]
	}
	rawName := strings.TrimSuffix(filepath.Base(strings.TrimRight(path, "/")), ".git")
	name, err := normalizeApplicationName(rawName)
	if err != nil {
		return "", fmt.Errorf("derive repository name from %s: %w", publicGitOrigin(input), err)
	}
	return name, nil
}

func isRemoteGitSource(input string) bool {
	if strings.HasPrefix(input, "git@") && strings.Contains(input, ":") {
		return true
	}
	parsed, err := url.Parse(input)
	if err != nil || parsed.Host == "" {
		return false
	}
	switch parsed.Scheme {
	case "http", "https", "ssh", "git":
		return true
	default:
		return false
	}
}

func publicGitOrigin(input string) string {
	parsed, err := url.Parse(input)
	if err != nil || parsed.Host == "" {
		return input
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
