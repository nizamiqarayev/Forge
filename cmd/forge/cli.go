package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const healthWait = 15 * time.Second

const usage = `Forge operates local application workloads.

Usage:
  forge deploy [options] <application-path>
  forge <status|logs|stop|delete> <application-name>

Available commands:
  deploy  Build and start an application
  status  Show an application's container status
  logs    Show an application's container logs
  stop    Stop an application gracefully
  delete  Delete a stopped application container
  help    Show this help message

Deploy options:
  --port PORT    Host port to publish (default 8080)
  --image IMAGE  Pull and deploy an existing container image
`

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return runWithDependencies(
		ctx,
		args,
		stdout,
		stderr,
		execDockerRunner{},
		newHTTPHealthChecker(),
		fileApplicationManifestLoader{},
	)
}

func runWithDependencies(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	docker dockerRunner,
	health healthChecker,
	manifests applicationManifestLoader,
) error {
	if len(args) == 0 {
		return writeUsage(stdout)
	}

	switch args[0] {
	case "help", "-h", "--help":
		return writeUsage(stdout)
	case "deploy":
		return runDeploy(ctx, args[1:], stdout, stderr, docker, health, manifests)
	case "status":
		return runStatus(ctx, args[1:], stdout, docker, health)
	case "logs":
		return runLogs(ctx, args[1:], stdout, stderr, docker)
	case "stop":
		return runStop(ctx, args[1:], stdout, stderr, docker)
	case "delete":
		return runDelete(ctx, args[1:], stdout, stderr, docker)
	default:
		return fmt.Errorf("unknown command %q; run %q for usage", args[0], "forge help")
	}
}

func runDeploy(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	docker dockerRunner,
	health healthChecker,
	manifests applicationManifestLoader,
) error {
	flags := flag.NewFlagSet("deploy", flag.ContinueOnError)
	flags.SetOutput(stderr)
	port := flags.Int("port", 8080, "host port to publish")
	imageRef := flags.String("image", "", "existing container image to deploy")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse deploy options: %w", err)
	}

	applicationPath, err := validateApplicationPathArgs(flags.Args())
	if err != nil {
		return err
	}
	if *port < 1 || *port > 65535 {
		return fmt.Errorf("deploy port must be between 1 and 65535")
	}

	manifest, err := manifests.Load(applicationPath)
	if err != nil {
		return fmt.Errorf("load application: %w", err)
	}
	app := manifest.Name
	container := containerName(app)
	localImage := app + ":local"
	dockerfile := filepath.Join(applicationPath, manifest.Dockerfile)

	existing, err := docker.Output(
		ctx,
		"container", "ls",
		"--all",
		"--quiet",
		"--filter", "name=^/"+container+"$",
	)
	if err != nil {
		return fmt.Errorf("check existing %s deployment: %w", app, err)
	}
	if strings.TrimSpace(existing) != "" {
		return fmt.Errorf("%s is already deployed; stop and delete it before deploying again", app)
	}

	selectedImage := strings.TrimSpace(*imageRef)
	if selectedImage == "" {
		selectedImage = localImage

		if err := writeString(stdout, "Building "+app+" image...\n", "deploy output"); err != nil {
			return err
		}
		if err := docker.Run(
			ctx,
			stdout,
			stderr,
			"build",
			"--file", dockerfile,
			"--tag", selectedImage,
			applicationPath,
		); err != nil {
			return fmt.Errorf("build %s image: %w", app, err)
		}

		if err := writeString(stdout, "Built "+selectedImage+"\n", "deploy output"); err != nil {
			return err
		}
	} else {
		if err := writeString(stdout, "Pulling "+selectedImage+"...\n", "deploy output"); err != nil {
			return err
		}
		if err := docker.Run(ctx, stdout, stderr, "pull", selectedImage); err != nil {
			return fmt.Errorf("pull %s image: %w", app, err)
		}
		if err := writeString(stdout, "Pulled "+selectedImage+"\n", "deploy output"); err != nil {
			return err
		}
	}

	if err := writeString(stdout, "Starting "+app+"...\n", "deploy output"); err != nil {
		return err
	}
	if err := docker.Run(
		ctx,
		stdout,
		stderr,
		"run",
		"--detach",
		"--name", container,
		"--label", "forge.managed=true",
		"--label", "forge.app="+app,
		"--label", "forge.host-port="+strconv.Itoa(*port),
		"--label", "forge.health-path="+manifest.HealthPath,
		"--publish", strconv.Itoa(*port)+":"+strconv.Itoa(manifest.ContainerPort),
		selectedImage,
	); err != nil {
		return fmt.Errorf("start %s container: %w", app, err)
	}

	healthURL := fmt.Sprintf("http://127.0.0.1:%d%s", *port, manifest.HealthPath)
	if err := writeString(stdout, "Waiting for "+app+" health...\n", "deploy output"); err != nil {
		return err
	}
	healthCtx, cancel := context.WithTimeout(ctx, healthWait)
	defer cancel()
	if err := health.Wait(healthCtx, healthURL); err != nil {
		return fmt.Errorf("wait for %s health: %w", app, err)
	}

	return writeString(
		stdout,
		fmt.Sprintf("Deployed %s\nContainer: %s\nURL: http://localhost:%d\n", app, container, *port),
		"deploy output",
	)
}

func runStatus(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	docker dockerRunner,
	health healthChecker,
) error {
	app, err := validateAppArgs("status", args)
	if err != nil {
		return err
	}

	container := containerName(app)
	state, err := docker.Output(
		ctx,
		"container", "inspect",
		"--format", `{{.State.Status}}|{{index .Config.Labels "forge.host-port"}}|{{.Config.Image}}|{{index .Config.Labels "forge.health-path"}}`,
		container,
	)
	if err != nil {
		return fmt.Errorf("inspect %s deployment: %w", app, err)
	}

	parts := strings.SplitN(strings.TrimSpace(state), "|", 4)
	if len(parts) != 4 {
		return fmt.Errorf("inspect %s deployment: unexpected Docker output %q", app, state)
	}
	containerState, hostPort, deployedImage, healthPath := parts[0], parts[1], parts[2], parts[3]
	healthState := "unavailable"
	url := "-"
	if hostPort != "" {
		url = "http://localhost:" + hostPort
	}
	if containerState == "running" && hostPort != "" && healthPath != "" {
		healthCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := health.Wait(healthCtx, "http://127.0.0.1:"+hostPort+healthPath)
		cancel()
		if err == nil {
			healthState = "healthy"
		} else {
			healthState = "unhealthy"
		}
	}

	return writeString(
		stdout,
		fmt.Sprintf(
			"Container: %s\nStatus: %s\nImage: %s\nURL: %s\nHealth: %s\n",
			container,
			containerState,
			deployedImage,
			url,
			healthState,
		),
		"status output",
	)
}

func runLogs(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	docker dockerRunner,
) error {
	app, err := validateAppArgs("logs", args)
	if err != nil {
		return err
	}
	if err := docker.Run(ctx, stdout, stderr, "logs", containerName(app)); err != nil {
		return fmt.Errorf("read %s logs: %w", app, err)
	}
	return nil
}

func runStop(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	docker dockerRunner,
) error {
	app, err := validateAppArgs("stop", args)
	if err != nil {
		return err
	}
	if err := docker.Run(ctx, stdout, stderr, "stop", "--timeout", "10", containerName(app)); err != nil {
		return fmt.Errorf("stop %s: %w", app, err)
	}
	return writeString(stdout, "Stopped "+app+"\n", "stop output")
}

func runDelete(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	docker dockerRunner,
) error {
	app, err := validateAppArgs("delete", args)
	if err != nil {
		return err
	}
	if err := docker.Run(ctx, stdout, stderr, "container", "rm", containerName(app)); err != nil {
		return fmt.Errorf("delete %s: %w", app, err)
	}
	return writeString(stdout, "Deleted "+app+" container\n", "delete output")
}

func validateAppArgs(command string, args []string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("%s expects exactly one app name", command)
	}
	app := strings.TrimSpace(args[0])
	if !applicationNamePattern.MatchString(app) {
		return "", fmt.Errorf("invalid app name %q", args[0])
	}
	return app, nil
}

func validateApplicationPathArgs(args []string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("deploy expects exactly one application path")
	}
	applicationPath := strings.TrimSpace(args[0])
	if applicationPath == "" {
		return "", fmt.Errorf("deploy application path must not be empty")
	}
	return filepath.Clean(applicationPath), nil
}

func containerName(app string) string {
	return "forge-" + app
}

func writeUsage(w io.Writer) error {
	return writeString(w, usage, "usage")
}

func writeString(w io.Writer, value, description string) error {
	if _, err := io.WriteString(w, value); err != nil {
		return fmt.Errorf("write %s: %w", description, err)
	}
	return nil
}
