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

const composeWaitSeconds = 60

const usage = `Forge operates local application workloads.

Usage:
  forge deploy [options] <application-path-or-git-url>
  forge <status|logs|stop|delete> <application-name>

Available commands:
  deploy  Build and start an application
  status  Show an application's container status
  logs    Show an application's container logs
  stop    Stop an application gracefully
  delete  Delete a stopped application container
  help    Show this help message

Deploy options:
  --port PORT      Host port to publish (default 8080)
  --image IMAGE    Pull and deploy an existing container image
  --workspace DIR  Store managed checkouts in DIR (default .forge)
`

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return runWithDependencies(
		ctx,
		args,
		stdout,
		stderr,
		execDockerRunner{},
		newHTTPHealthChecker(),
		newFileDeploymentPlanResolver(),
	)
}

func runWithDependencies(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	docker dockerRunner,
	health healthChecker,
	plans deploymentPlanResolver,
) error {
	if len(args) == 0 {
		return writeUsage(stdout)
	}

	switch args[0] {
	case "help", "-h", "--help":
		return writeUsage(stdout)
	case "deploy":
		return runDeploy(ctx, args[1:], stdout, stderr, docker, health, plans)
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
	plans deploymentPlanResolver,
) error {
	flags := flag.NewFlagSet("deploy", flag.ContinueOnError)
	flags.SetOutput(stderr)
	port := flags.Int("port", 8080, "host port to publish")
	imageRef := flags.String("image", "", "existing container image to deploy")
	workspace := flags.String("workspace", ".forge", "managed checkout directory")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse deploy options: %w", err)
	}

	deploymentSource, err := validateDeploymentSourceArgs(flags.Args())
	if err != nil {
		return err
	}
	if *port < 1 || *port > 65535 {
		return fmt.Errorf("deploy port must be between 1 and 65535")
	}
	providedOptions := make(map[string]bool)
	flags.Visit(func(option *flag.Flag) {
		providedOptions[option.Name] = true
	})

	if isRemoteGitSource(deploymentSource) {
		if err := writeString(stdout, "Fetching "+publicGitOrigin(deploymentSource)+"...\n", "deploy output"); err != nil {
			return err
		}
	}
	plan, err := plans.Resolve(ctx, deploymentSource, *workspace)
	if err != nil {
		return fmt.Errorf("resolve application: %w", err)
	}

	switch plan.Driver {
	case composeDriver:
		if providedOptions["port"] {
			return fmt.Errorf("--port cannot override a Compose deployment; publish ports in %s", plan.ComposeFile)
		}
		if strings.TrimSpace(*imageRef) != "" {
			return fmt.Errorf("--image cannot override a Compose deployment; configure service images in %s", plan.ComposeFile)
		}
		return runComposeDeploy(ctx, stdout, stderr, docker, plan)
	case containerDriver:
		return runContainerDeploy(ctx, stdout, stderr, docker, health, plan, *port, *imageRef)
	default:
		return fmt.Errorf("unsupported deployment driver %q", plan.Driver)
	}
}

func runContainerDeploy(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	docker dockerRunner,
	health healthChecker,
	plan deploymentPlan,
	port int,
	imageRef string,
) error {
	app := plan.Name
	container := containerName(app)
	localImage := app + ":local"
	dockerfile := filepath.Join(plan.ApplicationPath, plan.Dockerfile)

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

	selectedImage := strings.TrimSpace(imageRef)
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
			plan.ApplicationPath,
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
	runArgs := []string{
		"run",
		"--detach",
		"--name", container,
		"--label", "forge.managed=true",
		"--label", "forge.app=" + app,
		"--label", "forge.host-port=" + strconv.Itoa(port),
		"--label", "forge.health-path=" + plan.HealthPath,
	}
	if plan.Source != "" {
		runArgs = append(runArgs, "--label", "forge.source="+plan.Source)
	}
	if plan.Revision != "" {
		runArgs = append(runArgs, "--label", "forge.revision="+plan.Revision)
	}
	runArgs = append(
		runArgs,
		"--publish", strconv.Itoa(port)+":"+strconv.Itoa(plan.ContainerPort),
		selectedImage,
	)
	if err := docker.Run(
		ctx,
		stdout,
		stderr,
		runArgs...,
	); err != nil {
		return fmt.Errorf("start %s container: %w", app, err)
	}

	healthURL := fmt.Sprintf("http://127.0.0.1:%d%s", port, plan.HealthPath)
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
		fmt.Sprintf("Deployed %s\nContainer: %s\nURL: http://localhost:%d\n%s", app, container, port, deploymentRevisionOutput(plan)),
		"deploy output",
	)
}

func runComposeDeploy(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	docker dockerRunner,
	plan deploymentPlan,
) error {
	project := containerName(plan.Name)
	composeFile := filepath.Join(plan.ApplicationPath, plan.ComposeFile)
	composeArgs := []string{
		"compose",
		"--project-name", project,
		"--file", composeFile,
	}

	existing, err := docker.Output(
		ctx,
		append(composeArgs, "ps", "--all", "--quiet")...,
	)
	if err != nil {
		return fmt.Errorf("check existing %s Compose deployment: %w", plan.Name, err)
	}
	if strings.TrimSpace(existing) != "" {
		return fmt.Errorf("%s is already deployed as Compose project %s", plan.Name, project)
	}

	if err := writeString(stdout, "Building and starting "+plan.Name+" with Docker Compose...\n", "deploy output"); err != nil {
		return err
	}
	upArgs := append(
		composeArgs,
		"up",
		"--detach",
		"--build",
		"--wait",
		"--wait-timeout", strconv.Itoa(composeWaitSeconds),
	)
	if err := docker.Run(ctx, stdout, stderr, upArgs...); err != nil {
		return fmt.Errorf("deploy %s Compose project: %w", plan.Name, err)
	}

	return writeString(
		stdout,
		fmt.Sprintf("Deployed %s\nCompose project: %s\nCompose file: %s\n%s", plan.Name, project, composeFile, deploymentRevisionOutput(plan)),
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
		"--format", `{{.State.Status}}|{{index .Config.Labels "forge.host-port"}}|{{.Config.Image}}|{{index .Config.Labels "forge.health-path"}}|{{index .Config.Labels "forge.source"}}|{{index .Config.Labels "forge.revision"}}`,
		container,
	)
	if err != nil {
		compose, found, composeErr := findComposeDeployment(ctx, docker, app)
		if composeErr != nil {
			return composeErr
		}
		if !found {
			return fmt.Errorf("inspect %s deployment: %w", app, err)
		}
		status, err := docker.Output(ctx, append(compose.args(), "ps", "--all")...)
		if err != nil {
			return fmt.Errorf("inspect %s Compose deployment: %w", app, err)
		}
		return writeString(
			stdout,
			fmt.Sprintf("Application: %s\nDriver: compose\nProject: %s\n%s", app, compose.project, status),
			"status output",
		)
	}

	parts := strings.SplitN(strings.TrimSpace(state), "|", 6)
	if len(parts) != 6 {
		return fmt.Errorf("inspect %s deployment: unexpected Docker output %q", app, state)
	}
	containerState, hostPort, deployedImage, healthPath := parts[0], parts[1], parts[2], parts[3]
	source, revision := parts[4], parts[5]
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
			"Container: %s\nStatus: %s\nImage: %s\nURL: %s\nHealth: %s\n%s",
			container,
			containerState,
			deployedImage,
			url,
			healthState,
			deploymentRevisionOutput(deploymentPlan{Source: source, Revision: revision}),
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
	container := containerName(app)
	if _, err := docker.Output(ctx, "container", "inspect", "--format", "{{.Id}}", container); err == nil {
		if err := docker.Run(ctx, stdout, stderr, "logs", container); err != nil {
			return fmt.Errorf("read %s logs: %w", app, err)
		}
		return nil
	}
	compose, found, err := findComposeDeployment(ctx, docker, app)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%s deployment not found", app)
	}
	if err := docker.Run(ctx, stdout, stderr, append(compose.args(), "logs")...); err != nil {
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
	container := containerName(app)
	if _, err := docker.Output(ctx, "container", "inspect", "--format", "{{.Id}}", container); err == nil {
		if err := docker.Run(ctx, stdout, stderr, "stop", "--timeout", "10", container); err != nil {
			return fmt.Errorf("stop %s: %w", app, err)
		}
		return writeString(stdout, "Stopped "+app+"\n", "stop output")
	}
	compose, found, err := findComposeDeployment(ctx, docker, app)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%s deployment not found", app)
	}
	if err := docker.Run(ctx, stdout, stderr, append(compose.args(), "stop")...); err != nil {
		return fmt.Errorf("stop %s Compose deployment: %w", app, err)
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
	container := containerName(app)
	if _, err := docker.Output(ctx, "container", "inspect", "--format", "{{.Id}}", container); err == nil {
		if err := docker.Run(ctx, stdout, stderr, "container", "rm", container); err != nil {
			return fmt.Errorf("delete %s: %w", app, err)
		}
		return writeString(stdout, "Deleted "+app+" container\n", "delete output")
	}
	compose, found, err := findComposeDeployment(ctx, docker, app)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%s deployment not found", app)
	}
	if err := docker.Run(ctx, stdout, stderr, append(compose.args(), "down", "--remove-orphans")...); err != nil {
		return fmt.Errorf("delete %s Compose deployment: %w", app, err)
	}
	return writeString(stdout, "Deleted "+app+" Compose deployment\n", "delete output")
}

type composeDeployment struct {
	project string
	files   []string
}

func (deployment composeDeployment) args() []string {
	args := []string{"compose", "--project-name", deployment.project}
	for _, file := range deployment.files {
		args = append(args, "--file", file)
	}
	return args
}

func findComposeDeployment(
	ctx context.Context,
	docker dockerRunner,
	app string,
) (composeDeployment, bool, error) {
	project := containerName(app)
	output, err := docker.Output(
		ctx,
		"container", "ls",
		"--all",
		"--filter", "label=com.docker.compose.project="+project,
		"--format", `{{.Label "com.docker.compose.project.config_files"}}`,
	)
	if err != nil {
		return composeDeployment{}, false, fmt.Errorf("discover %s Compose deployment: %w", app, err)
	}

	seen := make(map[string]bool)
	var files []string
	for _, line := range strings.Split(output, "\n") {
		for _, file := range strings.Split(line, ",") {
			file = strings.TrimSpace(file)
			if file != "" && !seen[file] {
				seen[file] = true
				files = append(files, file)
			}
		}
	}
	if len(files) == 0 {
		return composeDeployment{}, false, nil
	}
	return composeDeployment{project: project, files: files}, true, nil
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

func validateDeploymentSourceArgs(args []string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("deploy expects exactly one application path or Git URL")
	}
	source := strings.TrimSpace(args[0])
	if source == "" {
		return "", fmt.Errorf("deploy source must not be empty")
	}
	if isRemoteGitSource(source) {
		return source, nil
	}
	return filepath.Clean(source), nil
}

func deploymentRevisionOutput(plan deploymentPlan) string {
	if plan.Source == "" || plan.Revision == "" {
		return ""
	}
	return fmt.Sprintf("Source: %s\nRevision: %s\n", plan.Source, plan.Revision)
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
