package main

import (
	"context"
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

const (
	defaultContainerPort = 8080
	defaultHealthPath    = "/healthz"
	defaultDockerfile    = "Dockerfile"
)

var composeFilenames = map[string]struct{}{
	"compose.yaml":        {},
	"compose.yml":         {},
	"docker-compose.yaml": {},
	"docker-compose.yml":  {},
}

var applicationNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

type applicationManifest struct {
	Name          string `json:"name"`
	ContainerPort int    `json:"containerPort"`
	HealthPath    string `json:"healthPath"`
	Dockerfile    string `json:"dockerfile"`
}

type deploymentDriver string

const (
	containerDriver deploymentDriver = "container"
	composeDriver   deploymentDriver = "compose"
)

type deploymentPlan struct {
	Name            string
	Driver          deploymentDriver
	ApplicationPath string
	Workspace       string
	Source          string
	Revision        string
	Dockerfile      string
	ComposeFile     string
	ContainerPort   int
	HealthPath      string
}

type deploymentPlanResolver interface {
	Resolve(ctx context.Context, source, workspace string) (deploymentPlan, error)
}

type fileDeploymentPlanResolver struct {
	sources deploymentSourceResolver
}

func newFileDeploymentPlanResolver() fileDeploymentPlanResolver {
	return fileDeploymentPlanResolver{
		sources: fileDeploymentSourceResolver{git: execGitRunner{}},
	}
}

func (r fileDeploymentPlanResolver) Resolve(ctx context.Context, input, workspace string) (deploymentPlan, error) {
	source, err := r.sources.Resolve(ctx, input, workspace)
	if err != nil {
		return deploymentPlan{}, err
	}
	plan, err := resolveDeploymentPlan(source.Path)
	if err != nil {
		return deploymentPlan{}, err
	}
	plan.Source = source.Origin
	plan.Revision = source.Revision
	return plan, nil
}

func resolveDeploymentPlan(applicationPath string) (deploymentPlan, error) {
	applicationPath = filepath.Clean(applicationPath)
	info, err := os.Stat(applicationPath)
	if err != nil {
		return deploymentPlan{}, fmt.Errorf("inspect application directory %s: %w", applicationPath, err)
	}
	if !info.IsDir() {
		return deploymentPlan{}, fmt.Errorf("application path %s is not a directory", applicationPath)
	}

	manifestPath := filepath.Join(applicationPath, manifestFilename)
	if _, err := os.Stat(manifestPath); err == nil {
		manifest, err := loadApplicationManifest(applicationPath)
		if err != nil {
			return deploymentPlan{}, err
		}
		return deploymentPlan{
			Name:            manifest.Name,
			Driver:          containerDriver,
			ApplicationPath: applicationPath,
			Dockerfile:      manifest.Dockerfile,
			ContainerPort:   manifest.ContainerPort,
			HealthPath:      manifest.HealthPath,
		}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return deploymentPlan{}, fmt.Errorf("inspect manifest %s: %w", manifestPath, err)
	}

	name, err := applicationNameFromPath(applicationPath)
	if err != nil {
		return deploymentPlan{}, err
	}

	composeFiles, err := findComposeFiles(applicationPath)
	if err != nil {
		return deploymentPlan{}, err
	}
	switch len(composeFiles) {
	case 1:
		return deploymentPlan{
			Name:            name,
			Driver:          composeDriver,
			ApplicationPath: applicationPath,
			ComposeFile:     composeFiles[0],
		}, nil
	case 0:
		// Continue to the single-container fallback.
	default:
		return deploymentPlan{}, fmt.Errorf(
			"multiple Compose files found (%s); add %s to choose the deployment configuration",
			strings.Join(composeFiles, ", "),
			manifestFilename,
		)
	}

	dockerfilePath := filepath.Join(applicationPath, defaultDockerfile)
	if info, err := os.Stat(dockerfilePath); err == nil {
		if !info.Mode().IsRegular() {
			return deploymentPlan{}, fmt.Errorf("dockerfile %s is not a regular file", dockerfilePath)
		}
		return deploymentPlan{
			Name:            name,
			Driver:          containerDriver,
			ApplicationPath: applicationPath,
			Dockerfile:      defaultDockerfile,
			ContainerPort:   defaultContainerPort,
			HealthPath:      defaultHealthPath,
		}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return deploymentPlan{}, fmt.Errorf("inspect dockerfile %s: %w", dockerfilePath, err)
	}

	return deploymentPlan{}, fmt.Errorf(
		"no %s, root %s, or Compose file found in %s",
		manifestFilename,
		defaultDockerfile,
		applicationPath,
	)
}

func applicationNameFromPath(applicationPath string) (string, error) {
	absolutePath, err := filepath.Abs(applicationPath)
	if err != nil {
		return "", fmt.Errorf("resolve application path %s: %w", applicationPath, err)
	}
	return normalizeApplicationName(filepath.Base(absolutePath))
}

func normalizeApplicationName(value string) (string, error) {
	var name strings.Builder
	previousHyphen := false
	for _, character := range strings.ToLower(value) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			name.WriteRune(character)
			previousHyphen = false
		} else if name.Len() > 0 && !previousHyphen {
			name.WriteByte('-')
			previousHyphen = true
		}
	}

	normalized := strings.Trim(name.String(), "-")
	if len(normalized) > 63 {
		normalized = strings.TrimRight(normalized[:63], "-")
	}
	if !applicationNamePattern.MatchString(normalized) {
		return "", fmt.Errorf("cannot derive a valid application name from %q; add %s", value, manifestFilename)
	}
	return normalized, nil
}

func findComposeFiles(applicationPath string) ([]string, error) {
	var matches []string
	err := filepath.WalkDir(applicationPath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && path != applicationPath {
			switch entry.Name() {
			case ".git", "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if _, ok := composeFilenames[entry.Name()]; !ok {
			return nil
		}
		relativePath, err := filepath.Rel(applicationPath, path)
		if err != nil {
			return err
		}
		matches = append(matches, relativePath)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s for Compose files: %w", applicationPath, err)
	}
	return matches, nil
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
