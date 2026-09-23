package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const deploymentRecordSchemaVersion = 1

type deploymentRecord struct {
	SchemaVersion   int              `json:"schemaVersion"`
	Name            string           `json:"name"`
	Driver          deploymentDriver `json:"driver"`
	Source          string           `json:"source,omitempty"`
	Revision        string           `json:"revision,omitempty"`
	ApplicationPath string           `json:"applicationPath"`
	Image           string           `json:"image,omitempty"`
	HostPort        int              `json:"hostPort,omitempty"`
	ContainerPort   int              `json:"containerPort,omitempty"`
	HealthPath      string           `json:"healthPath,omitempty"`
	Dockerfile      string           `json:"dockerfile,omitempty"`
	ComposeProject  string           `json:"composeProject,omitempty"`
	ComposeFiles    []string         `json:"composeFiles,omitempty"`
	DeployedAt      time.Time        `json:"deployedAt"`
}

type deploymentRecordStore interface {
	Save(workspace string, record deploymentRecord) error
}

type fileDeploymentRecordStore struct {
	now func() time.Time
}

func newFileDeploymentRecordStore() fileDeploymentRecordStore {
	return fileDeploymentRecordStore{now: time.Now}
}

func (store fileDeploymentRecordStore) Save(workspace string, record deploymentRecord) error {
	if !applicationNamePattern.MatchString(record.Name) {
		return fmt.Errorf("invalid deployment record name %q", record.Name)
	}
	if store.now == nil {
		store.now = time.Now
	}
	record.SchemaVersion = deploymentRecordSchemaVersion
	record.DeployedAt = store.now().UTC()

	directory := filepath.Join(workspace, "deployments")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create deployment record directory: %w", err)
	}

	temporary, err := os.CreateTemp(directory, record.Name+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary deployment record: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("protect temporary deployment record: %w", err)
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(record); err != nil {
		temporary.Close()
		return fmt.Errorf("encode deployment record: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close deployment record: %w", err)
	}

	recordPath := filepath.Join(directory, record.Name+".json")
	if err := os.Rename(temporaryPath, recordPath); err != nil {
		return fmt.Errorf("store deployment record: %w", err)
	}
	return nil
}
