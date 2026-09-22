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

func writeApplicationFixture(t *testing.T, manifest string) string {
	t.Helper()
	applicationPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(applicationPath, manifestFilename), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(applicationPath, "Dockerfile"), []byte("FROM scratch\n"), 0o600); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	return applicationPath
}
