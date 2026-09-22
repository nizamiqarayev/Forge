package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const manifestFilename = "forge.json"

var applicationNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

type applicationManifest struct {
	Name          string `json:"name"`
	ContainerPort int    `json:"containerPort"`
	HealthPath    string `json:"healthPath"`
	Dockerfile    string `json:"dockerfile"`
}

type applicationManifestLoader interface {
	Load(applicationPath string) (applicationManifest, error)
}

type fileApplicationManifestLoader struct{}

func (fileApplicationManifestLoader) Load(applicationPath string) (applicationManifest, error) {
	return loadApplicationManifest(applicationPath)
}

func loadApplicationManifest(applicationPath string) (applicationManifest, error) {
	manifestPath := filepath.Join(applicationPath, manifestFilename)
	file, err := os.Open(manifestPath)
	if err != nil {
		return applicationManifest{}, fmt.Errorf("open manifest %s: %w", manifestPath, err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()

	var manifest applicationManifest
	if err := decoder.Decode(&manifest); err != nil {
		return applicationManifest{}, fmt.Errorf("decode manifest %s: %w", manifestPath, err)
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return applicationManifest{}, fmt.Errorf("decode manifest %s: multiple JSON values", manifestPath)
		}
		return applicationManifest{}, fmt.Errorf("decode manifest %s: %w", manifestPath, err)
	}

	if err := validateApplicationManifest(applicationPath, &manifest); err != nil {
		return applicationManifest{}, fmt.Errorf("validate manifest %s: %w", manifestPath, err)
	}
	return manifest, nil
}

func validateApplicationManifest(applicationPath string, manifest *applicationManifest) error {
	manifest.Name = strings.TrimSpace(manifest.Name)
	if !applicationNamePattern.MatchString(manifest.Name) {
		return fmt.Errorf("name must contain 1-63 lowercase letters, numbers, or hyphens")
	}
	if manifest.ContainerPort < 1 || manifest.ContainerPort > 65535 {
		return fmt.Errorf("containerPort must be between 1 and 65535")
	}
	if manifest.HealthPath == "" || !strings.HasPrefix(manifest.HealthPath, "/") {
		return fmt.Errorf("healthPath must start with /")
	}

	manifest.Dockerfile = filepath.Clean(strings.TrimSpace(manifest.Dockerfile))
	if manifest.Dockerfile == "." {
		return fmt.Errorf("dockerfile is required")
	}
	if filepath.IsAbs(manifest.Dockerfile) ||
		manifest.Dockerfile == ".." ||
		strings.HasPrefix(manifest.Dockerfile, ".."+string(filepath.Separator)) {
		return fmt.Errorf("dockerfile must stay within the application directory")
	}

	dockerfilePath := filepath.Join(applicationPath, manifest.Dockerfile)
	info, err := os.Stat(dockerfilePath)
	if err != nil {
		return fmt.Errorf("inspect dockerfile %s: %w", dockerfilePath, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("dockerfile %s is not a regular file", dockerfilePath)
	}
	return nil
}
