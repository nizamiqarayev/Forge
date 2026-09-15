package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type dockerRunner interface {
	Run(
		ctx context.Context,
		stdout io.Writer,
		stderr io.Writer,
		args ...string,
	) error
	Output(ctx context.Context, args ...string) (string, error)
}

type execDockerRunner struct{}

func (execDockerRunner) Run(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	args ...string,
) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run docker %q: %w", args, err)
	}
	return nil
}

func (execDockerRunner) Output(ctx context.Context, args ...string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return "", fmt.Errorf("run docker %q: %s: %w", args, detail, err)
		}
		return "", fmt.Errorf("run docker %q: %w", args, err)
	}

	return stdout.String(), nil
}
