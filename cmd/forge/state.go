package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const deploymentRecordSchemaVersion = 1

var errDeploymentRecordNotFound = errors.New("deployment record not found")

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
	Load(workspace, name string) (deploymentRecord, error)
	History(workspace, name string) ([]deploymentRecord, error)
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

	current, err := store.Load(workspace, record.Name)
	if err == nil && !sameDeploymentRevision(current, record) {
		if err := store.archive(workspace, current); err != nil {
			return err
		}
	} else if err != nil && !errors.Is(err, errDeploymentRecordNotFound) {
		return fmt.Errorf("load current deployment record: %w", err)
	}

	return writeDeploymentRecord(filepath.Join(directory, record.Name+".json"), record)
}

func (store fileDeploymentRecordStore) Load(workspace, name string) (deploymentRecord, error) {
	if !applicationNamePattern.MatchString(name) {
		return deploymentRecord{}, fmt.Errorf("invalid deployment record name %q", name)
	}
	path := filepath.Join(workspace, "deployments", name+".json")
	record, err := readDeploymentRecord(path)
	if errors.Is(err, os.ErrNotExist) {
		return deploymentRecord{}, fmt.Errorf("%w: %s", errDeploymentRecordNotFound, name)
	}
	if err != nil {
		return deploymentRecord{}, err
	}
	if record.Name != name {
		return deploymentRecord{}, fmt.Errorf("deployment record %s contains name %q", path, record.Name)
	}
	return record, nil
}

func (store fileDeploymentRecordStore) History(workspace, name string) ([]deploymentRecord, error) {
	if !applicationNamePattern.MatchString(name) {
		return nil, fmt.Errorf("invalid deployment record name %q", name)
	}
	directory := filepath.Join(workspace, "deployments", name, "history")
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read deployment history: %w", err)
	}
	sort.Slice(entries, func(left, right int) bool {
		return entries[left].Name() > entries[right].Name()
	})

	records := make([]deploymentRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		record, err := readDeploymentRecord(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (store fileDeploymentRecordStore) archive(workspace string, record deploymentRecord) error {
	directory := filepath.Join(workspace, "deployments", record.Name, "history")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create deployment history directory: %w", err)
	}
	revision := record.Revision
	if revision == "" {
		revision = record.Image
	}
	revision = strings.Map(func(character rune) rune {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') {
			return character
		}
		return '-'
	}, revision)
	revision = strings.Trim(revision, "-")
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if revision == "" {
		revision = "unknown"
	}
	filename := record.DeployedAt.UTC().Format("20060102T150405.000000000Z") + "-" + revision + ".json"
	if err := writeDeploymentRecord(filepath.Join(directory, filename), record); err != nil {
		return fmt.Errorf("archive deployment record: %w", err)
	}
	return nil
}

func writeDeploymentRecord(path string, record deploymentRecord) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create deployment record directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, filepath.Base(path)+"-*.tmp")
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

	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("store deployment record: %w", err)
	}
	return nil
}

func readDeploymentRecord(path string) (deploymentRecord, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return deploymentRecord{}, err
	}
	var record deploymentRecord
	if err := json.Unmarshal(contents, &record); err != nil {
		return deploymentRecord{}, fmt.Errorf("decode deployment record %s: %w", path, err)
	}
	if record.SchemaVersion != deploymentRecordSchemaVersion {
		return deploymentRecord{}, fmt.Errorf("unsupported deployment record schema %d in %s", record.SchemaVersion, path)
	}
	return record, nil
}

func sameDeploymentRevision(left, right deploymentRecord) bool {
	if left.Driver != right.Driver {
		return false
	}
	if left.Revision != "" || right.Revision != "" {
		return left.Revision == right.Revision
	}
	return left.Image == right.Image && left.ApplicationPath == right.ApplicationPath
}

func isMissingDeploymentRecord(err error) bool {
	return errors.Is(err, errDeploymentRecordNotFound)
}
