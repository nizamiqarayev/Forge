package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

func runUpdate(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	docker dockerRunner,
	plans deploymentPlanResolver,
	records deploymentRecordStore,
) error {
	app, workspace, err := parseReleaseArgs("update", args, stderr)
	if err != nil {
		return err
	}
	current, err := records.Load(workspace, app)
	if err != nil {
		if isMissingDeploymentRecord(err) {
			return fmt.Errorf("%s has no deployment record; deploy it once with this Forge version", app)
		}
		return fmt.Errorf("load %s deployment: %w", app, err)
	}
	if !isRemoteGitSource(current.Source) {
		return fmt.Errorf("update requires a recorded remote Git source for %s", app)
	}
	if current.Driver != composeDriver {
		return fmt.Errorf("update currently supports Compose deployments; %s uses %s", app, current.Driver)
	}

	if err := writeString(stdout, "Fetching "+current.Source+"...\n", "update output"); err != nil {
		return err
	}
	plan, err := plans.Resolve(ctx, current.Source, workspace)
	if err != nil {
		return fmt.Errorf("resolve update: %w", err)
	}
	if plan.Name != app {
		return fmt.Errorf("update source resolved application %q, want %q", plan.Name, app)
	}
	if plan.Driver != current.Driver {
		return fmt.Errorf("update changes deployment driver from %s to %s", current.Driver, plan.Driver)
	}
	if plan.Revision == current.Revision {
		return fmt.Errorf("%s is already at revision %s", app, shortRevision(plan.Revision))
	}
	plan.Workspace = workspace
	next, err := composeDeploymentRecord(plan, current.ComposeProject)
	if err != nil {
		return err
	}

	if err := writeString(stdout, fmt.Sprintf("Updating %s from %s to %s...\n", app, shortRevision(current.Revision), shortRevision(next.Revision)), "update output"); err != nil {
		return err
	}
	if err := replaceComposeRelease(ctx, stdout, stderr, docker, current, next); err != nil {
		return err
	}
	if err := records.Save(workspace, next); err != nil {
		return fmt.Errorf("%s is updated but save deployment record: %w", app, err)
	}
	return writeString(stdout, fmt.Sprintf("Updated %s\nRevision: %s\n", app, next.Revision), "update output")
}

func runRollback(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	docker dockerRunner,
	records deploymentRecordStore,
) error {
	app, workspace, err := parseReleaseArgs("rollback", args, stderr)
	if err != nil {
		return err
	}
	current, err := records.Load(workspace, app)
	if err != nil {
		if isMissingDeploymentRecord(err) {
			return fmt.Errorf("%s has no deployment record; deploy it once with this Forge version", app)
		}
		return fmt.Errorf("load %s deployment: %w", app, err)
	}
	if current.Driver != composeDriver {
		return fmt.Errorf("rollback currently supports Compose deployments; %s uses %s", app, current.Driver)
	}
	history, err := records.History(workspace, app)
	if err != nil {
		return fmt.Errorf("load %s deployment history: %w", app, err)
	}
	var target deploymentRecord
	found := false
	for _, candidate := range history {
		if !sameDeploymentRevision(candidate, current) {
			target = candidate
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%s has no previous revision to roll back to", app)
	}

	if err := writeString(stdout, fmt.Sprintf("Rolling back %s from %s to %s...\n", app, shortRevision(current.Revision), shortRevision(target.Revision)), "rollback output"); err != nil {
		return err
	}
	if err := replaceComposeRelease(ctx, stdout, stderr, docker, current, target); err != nil {
		return err
	}
	if err := records.Save(workspace, target); err != nil {
		return fmt.Errorf("%s is rolled back but save deployment record: %w", app, err)
	}
	return writeString(stdout, fmt.Sprintf("Rolled back %s\nRevision: %s\n", app, target.Revision), "rollback output")
}

func runHistory(args []string, stdout, stderr io.Writer, records deploymentRecordStore) error {
	app, workspace, err := parseReleaseArgs("history", args, stderr)
	if err != nil {
		return err
	}
	current, err := records.Load(workspace, app)
	if err != nil {
		if isMissingDeploymentRecord(err) {
			return fmt.Errorf("%s has no deployment record; deploy it once with this Forge version", app)
		}
		return fmt.Errorf("load %s deployment: %w", app, err)
	}
	history, err := records.History(workspace, app)
	if err != nil {
		return fmt.Errorf("load %s deployment history: %w", app, err)
	}
	if err := writeString(stdout, "CURRENT\n"+formatDeploymentRecord(current), "history output"); err != nil {
		return err
	}
	if len(history) == 0 {
		return writeString(stdout, "HISTORY\n(empty)\n", "history output")
	}
	if err := writeString(stdout, "HISTORY\n", "history output"); err != nil {
		return err
	}
	for _, record := range history {
		if err := writeString(stdout, formatDeploymentRecord(record), "history output"); err != nil {
			return err
		}
	}
	return nil
}

func parseReleaseArgs(command string, args []string, stderr io.Writer) (string, string, error) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	workspace := flags.String("workspace", ".forge", "Forge workspace directory")
	if err := flags.Parse(args); err != nil {
		return "", "", fmt.Errorf("parse %s options: %w", command, err)
	}
	app, err := validateAppArgs(command, flags.Args())
	if err != nil {
		return "", "", err
	}
	absoluteWorkspace, err := filepath.Abs(*workspace)
	if err != nil {
		return "", "", fmt.Errorf("resolve Forge workspace %s: %w", *workspace, err)
	}
	return app, absoluteWorkspace, nil
}

func composeDeploymentRecord(plan deploymentPlan, project string) (deploymentRecord, error) {
	applicationPath, err := filepath.Abs(plan.ApplicationPath)
	if err != nil {
		return deploymentRecord{}, fmt.Errorf("resolve application path: %w", err)
	}
	composeFile, err := filepath.Abs(filepath.Join(plan.ApplicationPath, plan.ComposeFile))
	if err != nil {
		return deploymentRecord{}, fmt.Errorf("resolve Compose file: %w", err)
	}
	if project == "" {
		project = containerName(plan.Name)
	}
	return deploymentRecord{
		Name:            plan.Name,
		Driver:          composeDriver,
		Source:          plan.Source,
		Revision:        plan.Revision,
		ApplicationPath: applicationPath,
		ComposeProject:  project,
		ComposeFiles:    []string{composeFile},
	}, nil
}

func replaceComposeRelease(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	docker dockerRunner,
	current deploymentRecord,
	target deploymentRecord,
) error {
	if len(target.ComposeFiles) == 0 {
		return fmt.Errorf("target revision %s has no Compose files", shortRevision(target.Revision))
	}
	project := current.ComposeProject
	if project == "" {
		project = target.ComposeProject
	}
	if project == "" {
		project = containerName(current.Name)
	}
	if err := docker.Run(ctx, stdout, stderr, composeReleaseArgs(project, target.ComposeFiles)...); err == nil {
		return nil
	} else {
		updateErr := err
		if len(current.ComposeFiles) == 0 {
			return fmt.Errorf("apply revision %s: %w", shortRevision(target.Revision), updateErr)
		}
		if restoreErr := docker.Run(ctx, stdout, stderr, composeReleaseArgs(project, current.ComposeFiles)...); restoreErr != nil {
			return fmt.Errorf(
				"apply revision %s: %v; restore revision %s: %w",
				shortRevision(target.Revision),
				updateErr,
				shortRevision(current.Revision),
				restoreErr,
			)
		}
		return fmt.Errorf(
			"apply revision %s: %w; previous revision %s restored",
			shortRevision(target.Revision),
			updateErr,
			shortRevision(current.Revision),
		)
	}
}

func composeReleaseArgs(project string, files []string) []string {
	args := []string{"compose", "--project-name", project}
	for _, file := range files {
		args = append(args, "--file", file)
	}
	return append(
		args,
		"up",
		"--detach",
		"--build",
		"--wait",
		"--wait-timeout", fmt.Sprint(composeWaitSeconds),
		"--remove-orphans",
	)
}

func shortRevision(revision string) string {
	revision = strings.TrimSpace(revision)
	if len(revision) > 12 {
		return revision[:12]
	}
	if revision == "" {
		return "unknown"
	}
	return revision
}

func formatDeploymentRecord(record deploymentRecord) string {
	revision := record.Revision
	if revision == "" {
		revision = record.Image
	}
	deployedAt := record.DeployedAt.UTC().Format(time.RFC3339)
	return fmt.Sprintf("%s  %s  %s\n", shortRevision(revision), deployedAt, record.Driver)
}
