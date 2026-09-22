package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadApplicationManifest(t *testing.T) {
	t.Run("loads a valid manifest", func(t *testing.T) {
		applicationPath := writeApplicationFixture(t, `{
			"name": "hello-api",
			"containerPort": 8080,
			"healthPath": "/healthz",
			"dockerfile": "Dockerfile"
		}`)

		manifest, err := loadApplicationManifest(applicationPath)
		if err != nil {
			t.Fatalf("loadApplicationManifest() error = %v", err)
		}
		want := applicationManifest{
			Name:          "hello-api",
			ContainerPort: 8080,
			HealthPath:    "/healthz",
			Dockerfile:    "Dockerfile",
		}
		if manifest != want {
			t.Errorf("manifest = %#v, want %#v", manifest, want)
		}
	})

	for _, tt := range []struct {
		name      string
		manifest  string
		wantError string
	}{
		{
			name:      "rejects unknown fields",
			manifest:  `{"name":"hello-api","containerPort":8080,"healthPath":"/healthz","dockerfile":"Dockerfile","replicas":2}`,
			wantError: "unknown field",
		},
		{
			name:      "rejects invalid names",
			manifest:  `{"name":"Hello API","containerPort":8080,"healthPath":"/healthz","dockerfile":"Dockerfile"}`,
			wantError: "name must contain",
		},
		{
			name:      "rejects invalid ports",
			manifest:  `{"name":"hello-api","containerPort":0,"healthPath":"/healthz","dockerfile":"Dockerfile"}`,
			wantError: "containerPort must be",
		},
		{
			name:      "rejects relative health paths",
			manifest:  `{"name":"hello-api","containerPort":8080,"healthPath":"healthz","dockerfile":"Dockerfile"}`,
			wantError: "healthPath must start",
		},
		{
			name:      "rejects escaping dockerfiles",
			manifest:  `{"name":"hello-api","containerPort":8080,"healthPath":"/healthz","dockerfile":"../Dockerfile"}`,
			wantError: "dockerfile must stay within",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			applicationPath := writeApplicationFixture(t, tt.manifest)
			_, err := loadApplicationManifest(applicationPath)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want error containing %q", err, tt.wantError)
			}
		})
	}
}

func TestResolveDeploymentPlan(t *testing.T) {
	t.Run("uses manifest as an explicit override", func(t *testing.T) {
		applicationPath := filepath.Join(t.TempDir(), "Neutral API")
		mustMakeDir(t, applicationPath)
		mustWriteFile(t, filepath.Join(applicationPath, "Containerfile"), "FROM scratch\n")
		mustWriteFile(t, filepath.Join(applicationPath, "compose.yaml"), "services: {}\n")
		mustWriteFile(t, filepath.Join(applicationPath, manifestFilename), `{
			"name": "chosen-name",
			"containerPort": 9000,
			"healthPath": "/ready",
			"dockerfile": "Containerfile"
		}`)

		plan, err := resolveDeploymentPlan(applicationPath)
		if err != nil {
			t.Fatalf("resolveDeploymentPlan() error = %v", err)
		}
		want := deploymentPlan{
			Name:            "chosen-name",
			Driver:          containerDriver,
			ApplicationPath: applicationPath,
			Dockerfile:      "Containerfile",
			ContainerPort:   9000,
			HealthPath:      "/ready",
		}
		if plan != want {
			t.Errorf("plan = %#v, want %#v", plan, want)
		}
	})

	t.Run("defaults a root Dockerfile repository", func(t *testing.T) {
		applicationPath := filepath.Join(t.TempDir(), "Neutral API")
		mustMakeDir(t, applicationPath)
		mustWriteFile(t, filepath.Join(applicationPath, defaultDockerfile), "FROM scratch\n")

		plan, err := resolveDeploymentPlan(applicationPath)
		if err != nil {
			t.Fatalf("resolveDeploymentPlan() error = %v", err)
		}
		want := deploymentPlan{
			Name:            "neutral-api",
			Driver:          containerDriver,
			ApplicationPath: applicationPath,
			Dockerfile:      defaultDockerfile,
			ContainerPort:   defaultContainerPort,
			HealthPath:      defaultHealthPath,
		}
		if plan != want {
			t.Errorf("plan = %#v, want %#v", plan, want)
		}
	})

	t.Run("detects one nested Compose file", func(t *testing.T) {
		applicationPath := filepath.Join(t.TempDir(), "multi-service-app")
		composePath := filepath.Join(applicationPath, "deployments", "docker", "docker-compose.yml")
		mustMakeDir(t, filepath.Dir(composePath))
		mustWriteFile(t, composePath, "services: {}\n")

		plan, err := resolveDeploymentPlan(applicationPath)
		if err != nil {
			t.Fatalf("resolveDeploymentPlan() error = %v", err)
		}
		want := deploymentPlan{
			Name:            "multi-service-app",
			Driver:          composeDriver,
			ApplicationPath: applicationPath,
			ComposeFile:     filepath.Join("deployments", "docker", "docker-compose.yml"),
		}
		if plan != want {
			t.Errorf("plan = %#v, want %#v", plan, want)
		}
	})

	t.Run("rejects ambiguous Compose files", func(t *testing.T) {
		applicationPath := filepath.Join(t.TempDir(), "ambiguous-app")
		mustMakeDir(t, filepath.Join(applicationPath, "deploy"))
		mustWriteFile(t, filepath.Join(applicationPath, "compose.yaml"), "services: {}\n")
		mustWriteFile(t, filepath.Join(applicationPath, "deploy", "docker-compose.yml"), "services: {}\n")

		_, err := resolveDeploymentPlan(applicationPath)
		if err == nil || !strings.Contains(err.Error(), "multiple Compose files found") {
			t.Fatalf("error = %v, want ambiguity error", err)
		}
	})

	t.Run("ignores generated dependency directories", func(t *testing.T) {
		applicationPath := filepath.Join(t.TempDir(), "dockerfile-app")
		mustMakeDir(t, filepath.Join(applicationPath, "node_modules", "dependency"))
		mustWriteFile(t, filepath.Join(applicationPath, defaultDockerfile), "FROM scratch\n")
		mustWriteFile(t, filepath.Join(applicationPath, "node_modules", "dependency", "compose.yaml"), "services: {}\n")

		plan, err := resolveDeploymentPlan(applicationPath)
		if err != nil {
			t.Fatalf("resolveDeploymentPlan() error = %v", err)
		}
		if plan.Driver != containerDriver {
			t.Errorf("driver = %q, want %q", plan.Driver, containerDriver)
		}
	})

	t.Run("rejects unsupported repositories", func(t *testing.T) {
		applicationPath := filepath.Join(t.TempDir(), "unsupported-app")
		mustMakeDir(t, applicationPath)

		_, err := resolveDeploymentPlan(applicationPath)
		if err == nil || !strings.Contains(err.Error(), "no forge.json, root Dockerfile, or Compose file found") {
			t.Fatalf("error = %v, want unsupported-repository error", err)
		}
	})
}

func writeApplicationFixture(t *testing.T, manifest string) string {
	t.Helper()
	applicationPath := t.TempDir()
	mustWriteFile(t, filepath.Join(applicationPath, manifestFilename), manifest)
	mustWriteFile(t, filepath.Join(applicationPath, "Dockerfile"), "FROM scratch\n")
	return applicationPath
}

func mustMakeDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("create directory %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
