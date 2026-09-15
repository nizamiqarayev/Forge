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
	calls       []dockerCall
	output      string
	outputError error
	runErrors   []error
	runCount    int
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
	return f.output, f.outputError
}

func (f *fakeDockerRunner) record(method string, args []string) {
	copyOfArgs := append([]string(nil), args...)
	f.calls = append(f.calls, dockerCall{method: method, args: copyOfArgs})
}

var _ dockerRunner = (*fakeDockerRunner)(nil)

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

			err := runWithDocker(context.Background(), tt.args, &stdout, &stderr, &fakeDockerRunner{})
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

	err := runWithDocker(
		context.Background(),
		[]string{"launch"},
		&stdout,
		&stderr,
		&fakeDockerRunner{},
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
			"build", "--file", "examples/hello-api/Dockerfile", "--tag", "hello-api:local", ".",
		},
	}
	runCall := dockerCall{
		method: "Run",
		args: []string{
			"run", "--detach",
			"--name", "forge-hello-api",
			"--label", "forge.managed=true",
			"--label", "forge.app=hello-api",
			"--publish", "18080:8080",
			"hello-api:local",
		},
	}

	t.Run("builds and starts the application", func(t *testing.T) {
		docker := &fakeDockerRunner{}
		stdout, stderr, err := invoke(t, docker, "deploy", "--port", "18080", "hello-api")
		if err != nil {
			t.Fatalf("deploy error = %v", err)
		}

		wantOutput := "Building hello-api image...\n" +
			"Built hello-api:local\n" +
			"Starting hello-api...\n" +
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
	})

	t.Run("rejects an existing deployment", func(t *testing.T) {
		docker := &fakeDockerRunner{output: "container-id\n"}
		_, _, err := invoke(t, docker, "deploy", "hello-api")
		if err == nil || !strings.Contains(err.Error(), "already deployed") {
			t.Fatalf("error = %v, want existing-deployment error", err)
		}
		if !reflect.DeepEqual(docker.calls, []dockerCall{checkCall}) {
			t.Errorf("Docker calls = %#v, want only existence check", docker.calls)
		}
	})

	t.Run("returns a build failure", func(t *testing.T) {
		docker := &fakeDockerRunner{runErrors: []error{errors.New("build failed")}}
		stdout, _, err := invoke(t, docker, "deploy", "hello-api")
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

	for _, tt := range []struct {
		name      string
		args      []string
		wantError string
	}{
		{name: "missing application", args: []string{"deploy"}, wantError: "expects exactly one"},
		{name: "unsupported application", args: []string{"deploy", "payments"}, wantError: "unsupported app"},
		{name: "invalid port", args: []string{"deploy", "--port", "0", "hello-api"}, wantError: "port must be"},
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
		wantOutput string
		wantCalls  []dockerCall
	}{
		{
			name:       "status",
			args:       []string{"status", "hello-api"},
			docker:     &fakeDockerRunner{output: "Status: running\n"},
			wantOutput: "Status: running\n",
			wantCalls: []dockerCall{{
				method: "Output",
				args: []string{
					"container", "inspect",
					"--format", "Container: forge-hello-api\nStatus: {{.State.Status}}\nImage: {{.Config.Image}}\nPorts: {{json .NetworkSettings.Ports}}",
					"forge-hello-api",
				},
			}},
		},
		{
			name:       "logs",
			args:       []string{"logs", "hello-api"},
			docker:     &fakeDockerRunner{},
			wantCalls:  []dockerCall{{method: "Run", args: []string{"logs", "forge-hello-api"}}},
			wantOutput: "",
		},
		{
			name:       "stop",
			args:       []string{"stop", "hello-api"},
			docker:     &fakeDockerRunner{},
			wantOutput: "Stopped hello-api\n",
			wantCalls: []dockerCall{{
				method: "Run",
				args:   []string{"stop", "--timeout", "10", "forge-hello-api"},
			}},
		},
		{
			name:       "delete",
			args:       []string{"delete", "hello-api"},
			docker:     &fakeDockerRunner{},
			wantOutput: "Deleted hello-api container\n",
			wantCalls: []dockerCall{{
				method: "Run",
				args:   []string{"container", "rm", "forge-hello-api"},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := invoke(t, tt.docker, tt.args...)
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
		})
	}
}

func invoke(t *testing.T, docker dockerRunner, args ...string) (string, string, error) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithDocker(context.Background(), args, &stdout, &stderr, docker)
	return stdout.String(), stderr.String(), err
}
