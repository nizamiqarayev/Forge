package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

type dockerCall struct {
	method string
	args   []string
}

type fakeDockerRunner struct {
	calls        []dockerCall
	output       string
	outputError  error
	outputs      []string
	outputErrors []error
	outputCount  int
	runErrors    []error
	runCount     int
}

type fakeHealthChecker struct {
	urls []string
	err  error
}

type fakeDeploymentPlanResolver struct {
	plan       deploymentPlan
	err        error
	paths      []string
	workspaces []string
}

func (f *fakeDeploymentPlanResolver) Resolve(_ context.Context, source, workspace string) (deploymentPlan, error) {
	f.paths = append(f.paths, source)
	f.workspaces = append(f.workspaces, workspace)
	return f.plan, f.err
}

const helloAPIPath = "examples/hello-api"

var helloAPIPlan = deploymentPlan{
	Name:            "hello-api",
	Driver:          containerDriver,
	ApplicationPath: helloAPIPath,
	Dockerfile:      "Dockerfile",
	ContainerPort:   8080,
	HealthPath:      "/healthz",
}

func (f *fakeHealthChecker) Wait(_ context.Context, url string) error {
	f.urls = append(f.urls, url)
	return f.err
}

func (f *fakeDockerRunner) Run(
	_ context.Context,
	_ io.Writer,
	_ io.Writer,
	args ...string,
) error {
	f.record("Run", args)
	var err error
	if f.runCount < len(f.runErrors) {
		err = f.runErrors[f.runCount]
	}
	f.runCount++
	return err
}

func (f *fakeDockerRunner) Output(_ context.Context, args ...string) (string, error) {
	f.record("Output", args)
	if f.outputCount < len(f.outputs) || f.outputCount < len(f.outputErrors) {
		var output string
		var err error
		if f.outputCount < len(f.outputs) {
			output = f.outputs[f.outputCount]
		}
		if f.outputCount < len(f.outputErrors) {
			err = f.outputErrors[f.outputCount]
		}
		f.outputCount++
		return output, err
	}
	return f.output, f.outputError
}

func (f *fakeDockerRunner) record(method string, args []string) {
	copyOfArgs := append([]string(nil), args...)
	f.calls = append(f.calls, dockerCall{method: method, args: copyOfArgs})
}

var _ dockerRunner = (*fakeDockerRunner)(nil)
var _ healthChecker = (*fakeHealthChecker)(nil)
var _ deploymentPlanResolver = (*fakeDeploymentPlanResolver)(nil)

func TestRunShowsHelp(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
	}{
		{name: "no arguments"},
		{name: "help command", args: []string{"help"}},
		{name: "short help flag", args: []string{"-h"}},
		{name: "long help flag", args: []string{"--help"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			err := runWithDependencies(
				context.Background(),
				tt.args,
				&stdout,
				&stderr,
				&fakeDockerRunner{},
				&fakeHealthChecker{},
				&fakeDeploymentPlanResolver{plan: helloAPIPlan},
			)
			if err != nil {
				t.Fatalf("runWithDocker() error = %v", err)
			}
			if stdout.String() != usage {
				t.Errorf("stdout = %q, want usage", stdout.String())
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want no output", stderr.String())
			}
		})
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := runWithDependencies(
		context.Background(),
		[]string{"launch"},
		&stdout,
		&stderr,
		&fakeDockerRunner{},
		&fakeHealthChecker{},
		&fakeDeploymentPlanResolver{plan: helloAPIPlan},
	)
	if err == nil || !strings.Contains(err.Error(), `unknown command "launch"`) {
		t.Fatalf("error = %v, want unknown-command error", err)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("stdout = %q, stderr = %q; want no output", stdout.String(), stderr.String())
	}
}

func TestRunDeploy(t *testing.T) {
	checkCall := dockerCall{
		method: "Output",
		args: []string{
			"container", "ls", "--all", "--quiet", "--filter", "name=^/forge-hello-api$",
		},
	}
	buildCall := dockerCall{
		method: "Run",
		args: []string{
			"build", "--file", "examples/hello-api/Dockerfile", "--tag", "hello-api:local", helloAPIPath,
		},
	}
	runCall := dockerCall{
		method: "Run",
		args: []string{
			"run", "--detach",
			"--name", "forge-hello-api",
			"--label", "forge.managed=true",
			"--label", "forge.app=hello-api",
			"--label", "forge.host-port=18080",
			"--label", "forge.health-path=/healthz",
			"--publish", "18080:8080",
			"hello-api:local",
		},
	}

	t.Run("builds and starts the application", func(t *testing.T) {
		docker := &fakeDockerRunner{}
		health := &fakeHealthChecker{}
		stdout, stderr, err := invokeWithHealth(
			t,
			docker,
			health,
			"deploy", "--port", "18080", helloAPIPath,
		)
		if err != nil {
			t.Fatalf("deploy error = %v", err)
		}

		wantOutput := "Building hello-api image...\n" +
			"Built hello-api:local\n" +
			"Starting hello-api...\n" +
			"Waiting for hello-api health...\n" +
			"Deployed hello-api\n" +
			"Container: forge-hello-api\n" +
			"URL: http://localhost:18080\n"
		if stdout != wantOutput {
			t.Errorf("stdout = %q, want %q", stdout, wantOutput)
		}
		if stderr != "" {
			t.Errorf("stderr = %q, want no output", stderr)
		}

		wantCalls := []dockerCall{checkCall, buildCall, runCall}
		if !reflect.DeepEqual(docker.calls, wantCalls) {
			t.Errorf("Docker calls = %#v, want %#v", docker.calls, wantCalls)
		}
		wantHealthURLs := []string{"http://127.0.0.1:18080/healthz"}
		if !reflect.DeepEqual(health.urls, wantHealthURLs) {
			t.Errorf("health URLs = %#v, want %#v", health.urls, wantHealthURLs)
		}
	})

	t.Run("pulls and starts a published image", func(t *testing.T) {
		const imageRef = "ghcr.io/example/hello-api:abc123"

		docker := &fakeDockerRunner{}
		health := &fakeHealthChecker{}

		stdout, _, err := invokeWithHealth(
			t,
			docker,
			health,
			"deploy",
			"--port", "18080",
			"--image", imageRef,
			helloAPIPath,
		)
		if err != nil {
			t.Fatalf("deploy error = %v", err)
		}
		if strings.Contains(stdout, "Building") {
			t.Errorf("stdout = %q, must not build a supplied image", stdout)
		}

		wantCalls := []dockerCall{
			checkCall,
			{
				method: "Run",
				args:   []string{"pull", imageRef},
			},
			{
				method: "Run",
				args: []string{
					"run", "--detach",
					"--name", "forge-hello-api",
					"--label", "forge.managed=true",
					"--label", "forge.app=hello-api",
					"--label", "forge.host-port=18080",
					"--label", "forge.health-path=/healthz",
					"--publish", "18080:8080",
					imageRef,
				},
			},
		}
		if !reflect.DeepEqual(docker.calls, wantCalls) {
			t.Errorf("Docker calls = %#v, want %#v", docker.calls, wantCalls)
		}
	})

	t.Run("records a remote source revision", func(t *testing.T) {
		const repositoryURL = "https://github.com/example/hello-api.git"
		plan := helloAPIPlan
		plan.Source = repositoryURL
		plan.Revision = testRevision
		docker := &fakeDockerRunner{}
		health := &fakeHealthChecker{}
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		err := runWithDependencies(
			context.Background(),
			[]string{"deploy", repositoryURL},
			&stdout,
			&stderr,
			docker,
			health,
			&fakeDeploymentPlanResolver{plan: plan},
		)
		if err != nil {
			t.Fatalf("deploy error = %v", err)
		}
		if !strings.Contains(stdout.String(), "Fetching "+repositoryURL+"...") ||
			!strings.Contains(stdout.String(), "Revision: "+testRevision) {
			t.Errorf("stdout = %q, want fetch and revision details", stdout.String())
		}

		var startCall dockerCall
		for _, call := range docker.calls {
			if call.method == "Run" && len(call.args) > 0 && call.args[0] == "run" {
				startCall = call
			}
		}
		if !containsAdjacentArguments(startCall.args, "--label", "forge.source="+repositoryURL) {
			t.Errorf("start arguments = %#v, want source label", startCall.args)
		}
		if !containsAdjacentArguments(startCall.args, "--label", "forge.revision="+testRevision) {
			t.Errorf("start arguments = %#v, want revision label", startCall.args)
		}
	})

	t.Run("returns a pull failure", func(t *testing.T) {
		const imageRef = "ghcr.io/example/hello-api:abc123"

		docker := &fakeDockerRunner{runErrors: []error{errors.New("pull failed")}}
		stdout, _, err := invoke(
			t,
			docker,
			"deploy", "--image", imageRef, helloAPIPath,
		)
		if err == nil || !strings.Contains(err.Error(), "pull hello-api image") {
			t.Fatalf("error = %v, want wrapped pull error", err)
		}
		if stdout != "Pulling "+imageRef+"...\n" {
			t.Errorf("stdout = %q, want only pull-start message", stdout)
		}

		wantCalls := []dockerCall{
			checkCall,
			{method: "Run", args: []string{"pull", imageRef}},
		}
		if !reflect.DeepEqual(docker.calls, wantCalls) {
			t.Errorf("Docker calls = %#v, want %#v", docker.calls, wantCalls)
		}
	})

	t.Run("returns an unhealthy deployment", func(t *testing.T) {
		docker := &fakeDockerRunner{}
		health := &fakeHealthChecker{err: errors.New("health timeout")}
		stdout, _, err := invokeWithHealth(t, docker, health, "deploy", helloAPIPath)
		if err == nil || !strings.Contains(err.Error(), "wait for hello-api health") {
			t.Fatalf("error = %v, want wrapped health error", err)
		}
		if strings.Contains(stdout, "Deployed hello-api") {
			t.Errorf("stdout = %q, must not report successful deployment", stdout)
		}
		if len(health.urls) != 1 {
			t.Errorf("health calls = %#v, want one call", health.urls)
		}
	})

	t.Run("rejects an existing deployment", func(t *testing.T) {
		docker := &fakeDockerRunner{output: "container-id\n"}
		_, _, err := invoke(t, docker, "deploy", helloAPIPath)
		if err == nil || !strings.Contains(err.Error(), "already deployed") {
			t.Fatalf("error = %v, want existing-deployment error", err)
		}
		if !reflect.DeepEqual(docker.calls, []dockerCall{checkCall}) {
			t.Errorf("Docker calls = %#v, want only existence check", docker.calls)
		}
	})

	t.Run("returns a build failure", func(t *testing.T) {
		docker := &fakeDockerRunner{runErrors: []error{errors.New("build failed")}}
		stdout, _, err := invoke(t, docker, "deploy", helloAPIPath)
		if err == nil || !strings.Contains(err.Error(), "build hello-api image") {
			t.Fatalf("error = %v, want wrapped build error", err)
		}
		if stdout != "Building hello-api image...\n" {
			t.Errorf("stdout = %q, want only build-start message", stdout)
		}
		if !reflect.DeepEqual(docker.calls, []dockerCall{checkCall, buildCall}) {
			t.Errorf("Docker calls = %#v, want check and build", docker.calls)
		}
	})

	t.Run("returns a resolution failure before Docker", func(t *testing.T) {
		docker := &fakeDockerRunner{}
		resolver := &fakeDeploymentPlanResolver{err: errors.New("invalid repository")}
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		err := runWithDependencies(
			context.Background(),
			[]string{"deploy", helloAPIPath},
			&stdout,
			&stderr,
			docker,
			&fakeHealthChecker{},
			resolver,
		)
		if err == nil || !strings.Contains(err.Error(), "resolve application") {
			t.Fatalf("error = %v, want wrapped resolution error", err)
		}
		if !reflect.DeepEqual(resolver.paths, []string{helloAPIPath}) {
			t.Errorf("resolved paths = %#v, want %#v", resolver.paths, []string{helloAPIPath})
		}
		if len(docker.calls) != 0 {
			t.Errorf("Docker calls = %#v, want none", docker.calls)
		}
	})

	t.Run("builds and starts a Compose repository", func(t *testing.T) {
		docker := &fakeDockerRunner{}
		resolver := &fakeDeploymentPlanResolver{plan: deploymentPlan{
			Name:            "multi-service-app",
			Driver:          composeDriver,
			ApplicationPath: helloAPIPath,
			ComposeFile:     "deployments/docker/compose.yaml",
		}}
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		err := runWithDependencies(
			context.Background(),
			[]string{"deploy", helloAPIPath},
			&stdout,
			&stderr,
			docker,
			&fakeHealthChecker{},
			resolver,
		)
		if err != nil {
			t.Fatalf("deploy error = %v", err)
		}
		composeFile := "examples/hello-api/deployments/docker/compose.yaml"
		wantCalls := []dockerCall{
			{
				method: "Output",
				args: []string{
					"compose", "--project-name", "forge-multi-service-app", "--file", composeFile,
					"ps", "--all", "--quiet",
				},
			},
			{
				method: "Run",
				args: []string{
					"compose", "--project-name", "forge-multi-service-app", "--file", composeFile,
					"up", "--detach", "--build", "--wait", "--wait-timeout", "60",
				},
			},
		}
		if !reflect.DeepEqual(docker.calls, wantCalls) {
			t.Errorf("Docker calls = %#v, want %#v", docker.calls, wantCalls)
		}
		wantOutput := "Building and starting multi-service-app with Docker Compose...\n" +
			"Deployed multi-service-app\n" +
			"Compose project: forge-multi-service-app\n" +
			"Compose file: " + composeFile + "\n"
		if stdout.String() != wantOutput {
			t.Errorf("stdout = %q, want %q", stdout.String(), wantOutput)
		}
	})

	t.Run("rejects an existing Compose deployment", func(t *testing.T) {
		docker := &fakeDockerRunner{output: "container-id\n"}
		plan := deploymentPlan{
			Name:            "multi-service-app",
			Driver:          composeDriver,
			ApplicationPath: helloAPIPath,
			ComposeFile:     "compose.yaml",
		}
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		err := runComposeDeploy(context.Background(), &stdout, &stderr, docker, plan)
		if err == nil || !strings.Contains(err.Error(), "already deployed as Compose project") {
			t.Fatalf("error = %v, want existing Compose deployment error", err)
		}
		if len(docker.calls) != 1 || docker.calls[0].method != "Output" {
			t.Errorf("Docker calls = %#v, want only existence check", docker.calls)
		}
	})

	t.Run("returns a Compose startup failure", func(t *testing.T) {
		docker := &fakeDockerRunner{runErrors: []error{errors.New("compose failed")}}
		plan := deploymentPlan{
			Name:            "multi-service-app",
			Driver:          composeDriver,
			ApplicationPath: helloAPIPath,
			ComposeFile:     "compose.yaml",
		}
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		err := runComposeDeploy(context.Background(), &stdout, &stderr, docker, plan)
		if err == nil || !strings.Contains(err.Error(), "deploy multi-service-app Compose project") {
			t.Fatalf("error = %v, want wrapped Compose error", err)
		}
		if strings.Contains(stdout.String(), "Deployed multi-service-app") {
			t.Errorf("stdout = %q, must not report successful deployment", stdout.String())
		}
	})

	t.Run("rejects single-container overrides for Compose", func(t *testing.T) {
		plan := deploymentPlan{
			Name:            "multi-service-app",
			Driver:          composeDriver,
			ApplicationPath: helloAPIPath,
			ComposeFile:     "compose.yaml",
		}
		for _, tt := range []struct {
			name      string
			args      []string
			wantError string
		}{
			{name: "port", args: []string{"deploy", "--port", "18080", helloAPIPath}, wantError: "--port cannot override"},
			{name: "image", args: []string{"deploy", "--image", "example/image:tag", helloAPIPath}, wantError: "--image cannot override"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				docker := &fakeDockerRunner{}
				var stdout bytes.Buffer
				var stderr bytes.Buffer
				err := runWithDependencies(
					context.Background(),
					tt.args,
					&stdout,
					&stderr,
					docker,
					&fakeHealthChecker{},
					&fakeDeploymentPlanResolver{plan: plan},
				)
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error = %v, want error containing %q", err, tt.wantError)
				}
				if len(docker.calls) != 0 {
					t.Errorf("Docker calls = %#v, want none", docker.calls)
				}
			})
		}
	})

	for _, tt := range []struct {
		name      string
		args      []string
		wantError string
	}{
		{name: "missing application", args: []string{"deploy"}, wantError: "expects exactly one"},
		{name: "invalid port", args: []string{"deploy", "--port", "0", helloAPIPath}, wantError: "port must be"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			docker := &fakeDockerRunner{}
			_, _, err := invoke(t, docker, tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want error containing %q", err, tt.wantError)
			}
			if len(docker.calls) != 0 {
				t.Errorf("Docker calls = %#v, want none", docker.calls)
			}
		})
	}
}

func TestLifecycleCommands(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		docker     *fakeDockerRunner
		health     *fakeHealthChecker
		wantOutput string
		wantCalls  []dockerCall
		wantURLs   []string
	}{
		{
			name:       "status",
			args:       []string{"status", "hello-api"},
			docker:     &fakeDockerRunner{output: "running|18080|ghcr.io/example/hello-api:abc123|/healthz||\n"},
			health:     &fakeHealthChecker{},
			wantOutput: "Container: forge-hello-api\nStatus: running\nImage: ghcr.io/example/hello-api:abc123\nURL: http://localhost:18080\nHealth: healthy\n",
			wantURLs:   []string{"http://127.0.0.1:18080/healthz"},
			wantCalls: []dockerCall{{
				method: "Output",
				args: []string{
					"container", "inspect",
					"--format", `{{.State.Status}}|{{index .Config.Labels "forge.host-port"}}|{{.Config.Image}}|{{index .Config.Labels "forge.health-path"}}|{{index .Config.Labels "forge.source"}}|{{index .Config.Labels "forge.revision"}}`,
					"forge-hello-api",
				},
			}},
		},
		{
			name:       "stopped status",
			args:       []string{"status", "hello-api"},
			docker:     &fakeDockerRunner{output: "exited|18080|hello-api:local|/healthz||\n"},
			health:     &fakeHealthChecker{},
			wantOutput: "Container: forge-hello-api\nStatus: exited\nImage: hello-api:local\nURL: http://localhost:18080\nHealth: unavailable\n",
			wantCalls: []dockerCall{{
				method: "Output",
				args: []string{
					"container", "inspect",
					"--format", `{{.State.Status}}|{{index .Config.Labels "forge.host-port"}}|{{.Config.Image}}|{{index .Config.Labels "forge.health-path"}}|{{index .Config.Labels "forge.source"}}|{{index .Config.Labels "forge.revision"}}`,
					"forge-hello-api",
				},
			}},
		},
		{
			name:   "logs",
			args:   []string{"logs", "hello-api"},
			docker: &fakeDockerRunner{},
			health: &fakeHealthChecker{},
			wantCalls: []dockerCall{
				{method: "Output", args: []string{"container", "inspect", "--format", "{{.Id}}", "forge-hello-api"}},
				{method: "Run", args: []string{"logs", "forge-hello-api"}},
			},
			wantOutput: "",
		},
		{
			name:       "stop",
			args:       []string{"stop", "hello-api"},
			docker:     &fakeDockerRunner{},
			health:     &fakeHealthChecker{},
			wantOutput: "Stopped hello-api\n",
			wantCalls: []dockerCall{
				{method: "Output", args: []string{"container", "inspect", "--format", "{{.Id}}", "forge-hello-api"}},
				{method: "Run", args: []string{"stop", "--timeout", "10", "forge-hello-api"}},
			},
		},
		{
			name:       "delete",
			args:       []string{"delete", "hello-api"},
			docker:     &fakeDockerRunner{},
			health:     &fakeHealthChecker{},
			wantOutput: "Deleted hello-api container\n",
			wantCalls: []dockerCall{
				{method: "Output", args: []string{"container", "inspect", "--format", "{{.Id}}", "forge-hello-api"}},
				{method: "Run", args: []string{"container", "rm", "forge-hello-api"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := invokeWithHealth(t, tt.docker, tt.health, tt.args...)
			if err != nil {
				t.Fatalf("command error = %v", err)
			}
			if stdout != tt.wantOutput {
				t.Errorf("stdout = %q, want %q", stdout, tt.wantOutput)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want no output", stderr)
			}
			if !reflect.DeepEqual(tt.docker.calls, tt.wantCalls) {
				t.Errorf("Docker calls = %#v, want %#v", tt.docker.calls, tt.wantCalls)
			}
			if !reflect.DeepEqual(tt.health.urls, tt.wantURLs) {
				t.Errorf("health URLs = %#v, want %#v", tt.health.urls, tt.wantURLs)
			}
		})
	}
}

func TestComposeLifecycleCommands(t *testing.T) {
	const composeFile = "/workspace/.forge/repositories/revision/malcore/deployments/docker/docker-compose.yml"
	missingContainer := errors.New("no such container")
	discoveryCall := dockerCall{
		method: "Output",
		args: []string{
			"container", "ls",
			"--all",
			"--filter", "label=com.docker.compose.project=forge-malcore",
			"--format", `{{.Label "com.docker.compose.project.config_files"}}`,
		},
	}
	containerInspectCall := dockerCall{
		method: "Output",
		args:   []string{"container", "inspect", "--format", "{{.Id}}", "forge-malcore"},
	}
	composeArgs := []string{
		"compose", "--project-name", "forge-malcore", "--file", composeFile,
	}

	t.Run("status", func(t *testing.T) {
		const composeStatus = "NAME             SERVICE    STATUS\nmalcore-api      api        Up\n"
		docker := &fakeDockerRunner{
			outputs:      []string{"", composeFile + "\n" + composeFile + "\n", composeStatus},
			outputErrors: []error{missingContainer, nil, nil},
		}
		stdout, stderr, err := invoke(t, docker, "status", "malcore")
		if err != nil {
			t.Fatalf("status error = %v", err)
		}
		wantOutput := "Application: malcore\nDriver: compose\nProject: forge-malcore\n" + composeStatus
		if stdout != wantOutput || stderr != "" {
			t.Errorf("stdout = %q, stderr = %q, want %q and no stderr", stdout, stderr, wantOutput)
		}
		statusArgs := append(append([]string(nil), composeArgs...), "ps", "--all")
		wantCalls := []dockerCall{
			{
				method: "Output",
				args: []string{
					"container", "inspect",
					"--format", `{{.State.Status}}|{{index .Config.Labels "forge.host-port"}}|{{.Config.Image}}|{{index .Config.Labels "forge.health-path"}}|{{index .Config.Labels "forge.source"}}|{{index .Config.Labels "forge.revision"}}`,
					"forge-malcore",
				},
			},
			discoveryCall,
			{method: "Output", args: statusArgs},
		}
		if !reflect.DeepEqual(docker.calls, wantCalls) {
			t.Errorf("Docker calls = %#v, want %#v", docker.calls, wantCalls)
		}
	})

	for _, tt := range []struct {
		name       string
		command    string
		composeCmd []string
		wantOutput string
	}{
		{name: "logs", command: "logs", composeCmd: []string{"logs"}},
		{name: "stop", command: "stop", composeCmd: []string{"stop"}, wantOutput: "Stopped malcore\n"},
		{name: "delete", command: "delete", composeCmd: []string{"down", "--remove-orphans"}, wantOutput: "Deleted malcore Compose deployment\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			docker := &fakeDockerRunner{
				outputs:      []string{"", composeFile + "\n"},
				outputErrors: []error{missingContainer, nil},
			}
			stdout, stderr, err := invoke(t, docker, tt.command, "malcore")
			if err != nil {
				t.Fatalf("%s error = %v", tt.command, err)
			}
			if stdout != tt.wantOutput || stderr != "" {
				t.Errorf("stdout = %q, stderr = %q, want %q and no stderr", stdout, stderr, tt.wantOutput)
			}
			commandArgs := append(append([]string(nil), composeArgs...), tt.composeCmd...)
			wantCalls := []dockerCall{
				containerInspectCall,
				discoveryCall,
				{method: "Run", args: commandArgs},
			}
			if !reflect.DeepEqual(docker.calls, wantCalls) {
				t.Errorf("Docker calls = %#v, want %#v", docker.calls, wantCalls)
			}
		})
	}
}

func invoke(t *testing.T, docker dockerRunner, args ...string) (string, string, error) {
	return invokeWithHealth(t, docker, &fakeHealthChecker{}, args...)
}

func invokeWithHealth(
	t *testing.T,
	docker dockerRunner,
	health healthChecker,
	args ...string,
) (string, string, error) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithDependencies(
		context.Background(),
		args,
		&stdout,
		&stderr,
		docker,
		health,
		&fakeDeploymentPlanResolver{plan: helloAPIPlan},
	)
	return stdout.String(), stderr.String(), err
}

func containsAdjacentArguments(arguments []string, first, second string) bool {
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == first && arguments[index+1] == second {
			return true
		}
	}
	return false
}
