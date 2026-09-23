package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileDeploymentRecordStoreSave(t *testing.T) {
	workspace := t.TempDir()
	deployedAt := time.Date(2026, time.September, 23, 12, 0, 0, 0, time.FixedZone("test", 4*60*60))
	store := fileDeploymentRecordStore{now: func() time.Time { return deployedAt }}
	record := deploymentRecord{
		Name:            "malcore",
		Driver:          composeDriver,
		Source:          "https://github.com/example/malcore.git",
		Revision:        testRevision,
		ApplicationPath: "/workspace/.forge/repositories/revision/malcore",
		ComposeProject:  "forge-malcore",
		ComposeFiles:    []string{"/workspace/.forge/repositories/revision/malcore/compose.yml"},
	}

	if err := store.Save(workspace, record); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	recordPath := filepath.Join(workspace, "deployments", "malcore.json")
	contents, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read deployment record: %v", err)
	}
	var saved deploymentRecord
	if err := json.Unmarshal(contents, &saved); err != nil {
		t.Fatalf("decode deployment record: %v", err)
	}
	if saved.SchemaVersion != deploymentRecordSchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", saved.SchemaVersion, deploymentRecordSchemaVersion)
	}
	if !saved.DeployedAt.Equal(deployedAt.UTC()) {
		t.Errorf("deployedAt = %v, want %v", saved.DeployedAt, deployedAt.UTC())
	}
	if saved.Name != record.Name || saved.Revision != record.Revision || saved.ComposeProject != record.ComposeProject {
		t.Errorf("saved record = %#v, want deployment identity", saved)
	}
	info, err := os.Stat(recordPath)
	if err != nil {
		t.Fatalf("inspect deployment record: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("record permissions = %o, want 600", info.Mode().Perm())
	}
	temporaryFiles, err := filepath.Glob(filepath.Join(workspace, "deployments", "*.tmp"))
	if err != nil {
		t.Fatalf("find temporary deployment records: %v", err)
	}
	if len(temporaryFiles) != 0 {
		t.Errorf("temporary deployment records = %#v, want none", temporaryFiles)
	}
}
