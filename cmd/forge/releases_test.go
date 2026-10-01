package main

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	oldRevision = "1111111111111111111111111111111111111111"
	newRevision = "2222222222222222222222222222222222222222"
)

func TestRunUpdateCompose(t *testing.T) {
	current := composeRecordFixture(oldRevision, "/workspace/old/malcore/compose.yml")
	nextPlan := deploymentPlan{
		Name:            "malcore",
		Driver:          composeDriver,
		ApplicationPath: "/workspace/new/malcore",
		Source:          current.Source,
		Revision:        newRevision,
		ComposeFile:     "compose.yml",
	}
	records := &fakeDeploymentRecordStore{current: current}
	docker := &fakeDockerRunner{}
	resolver := &fakeDeploymentPlanResolver{plan: nextPlan}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := runUpdate(
		context.Background(),
		[]string{"--workspace", "/workspace/.forge", "malcore"},
		&stdout,
		&stderr,
		docker,
		resolver,
		records,
	)
	if err != nil {
		t.Fatalf("runUpdate() error = %v", err)
	}
	wantRun := dockerCall{
		method: "Run",
		args: []string{
			"compose", "--project-name", "forge-malcore",
			"--file", "/workspace/new/malcore/compose.yml",
			"up", "--detach", "--build", "--wait", "--wait-timeout", "60", "--remove-orphans",
		},
	}
	if !reflect.DeepEqual(docker.calls, []dockerCall{wantRun}) {
		t.Errorf("Docker calls = %#v, want %#v", docker.calls, []dockerCall{wantRun})
	}
	if len(records.records) != 1 || records.records[0].Revision != newRevision {
		t.Errorf("saved records = %#v, want new revision", records.records)
	}
	if !strings.Contains(stdout.String(), "Updated malcore") || !strings.Contains(stdout.String(), newRevision) {
		t.Errorf("stdout = %q, want update result", stdout.String())
	}
}

func TestRunUpdateRestoresFailedComposeRelease(t *testing.T) {
	current := composeRecordFixture(oldRevision, "/workspace/old/malcore/compose.yml")
	nextPlan := deploymentPlan{
		Name:            "malcore",
		Driver:          composeDriver,
		ApplicationPath: "/workspace/new/malcore",
		Source:          current.Source,
		Revision:        newRevision,
		ComposeFile:     "compose.yml",
	}
	records := &fakeDeploymentRecordStore{current: current}
	docker := &fakeDockerRunner{runErrors: []error{errors.New("new revision unhealthy"), nil}}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := runUpdate(
		context.Background(),
		[]string{"--workspace", "/workspace/.forge", "malcore"},
		&stdout,
		&stderr,
		docker,
		&fakeDeploymentPlanResolver{plan: nextPlan},
		records,
	)
	if err == nil || !strings.Contains(err.Error(), "previous revision 111111111111 restored") {
		t.Fatalf("error = %v, want restored-previous-revision error", err)
	}
	if len(docker.calls) != 2 {
		t.Fatalf("Docker calls = %#v, want update and restore", docker.calls)
	}
	if !containsAdjacentArguments(docker.calls[1].args, "--file", current.ComposeFiles[0]) {
		t.Errorf("restore arguments = %#v, want old Compose file", docker.calls[1].args)
	}
	if len(records.records) != 0 {
		t.Errorf("saved records = %#v, want none after failed update", records.records)
	}
}

func TestRunRollbackCompose(t *testing.T) {
	current := composeRecordFixture(newRevision, "/workspace/new/malcore/compose.yml")
	target := composeRecordFixture(oldRevision, "/workspace/old/malcore/compose.yml")
	records := &fakeDeploymentRecordStore{current: current, history: []deploymentRecord{target}}
	docker := &fakeDockerRunner{}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := runRollback(
		context.Background(),
		[]string{"--workspace", "/workspace/.forge", "malcore"},
		&stdout,
		&stderr,
		docker,
		records,
	)
	if err != nil {
		t.Fatalf("runRollback() error = %v", err)
	}
	if len(docker.calls) != 1 || !containsAdjacentArguments(docker.calls[0].args, "--file", target.ComposeFiles[0]) {
		t.Errorf("Docker calls = %#v, want previous Compose revision", docker.calls)
	}
	if len(records.records) != 1 || records.records[0].Revision != oldRevision {
		t.Errorf("saved records = %#v, want rolled-back revision", records.records)
	}
	if !strings.Contains(stdout.String(), "Rolled back malcore") {
		t.Errorf("stdout = %q, want rollback result", stdout.String())
	}
}

func TestRunHistory(t *testing.T) {
	current := composeRecordFixture(newRevision, "/workspace/new/malcore/compose.yml")
	current.DeployedAt = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	previous := composeRecordFixture(oldRevision, "/workspace/old/malcore/compose.yml")
	previous.DeployedAt = current.DeployedAt.Add(-time.Hour)
	records := &fakeDeploymentRecordStore{current: current, history: []deploymentRecord{previous}}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if err := runHistory([]string{"--workspace", "/workspace/.forge", "malcore"}, &stdout, &stderr, records); err != nil {
		t.Fatalf("runHistory() error = %v", err)
	}
	for _, want := range []string{"CURRENT", "222222222222", "HISTORY", "111111111111"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func composeRecordFixture(revision, composeFile string) deploymentRecord {
	return deploymentRecord{
		SchemaVersion:   deploymentRecordSchemaVersion,
		Name:            "malcore",
		Driver:          composeDriver,
		Source:          "https://github.com/example/malcore.git",
		Revision:        revision,
		ApplicationPath: "/workspace/malcore",
		ComposeProject:  "forge-malcore",
		ComposeFiles:    []string{composeFile},
		DeployedAt:      time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC),
	}
}
