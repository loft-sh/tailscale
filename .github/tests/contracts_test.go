// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package workflows

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type releaseWorkflow struct {
	On          map[string]any    `yaml:"on"`
	Permissions map[string]string `yaml:"permissions"`
	Jobs        map[string]struct {
		Env         map[string]string `yaml:"env"`
		Permissions map[string]string `yaml:"permissions"`
		Steps       []struct {
			Name string `yaml:"name"`
			Run  string `yaml:"run"`
			Uses string `yaml:"uses"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func loadReleaseWorkflow(t *testing.T, name string) releaseWorkflow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	var workflow releaseWorkflow
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow
}

func buildCommands(t *testing.T, workflow releaseWorkflow, jobName string) string {
	t.Helper()
	job, ok := workflow.Jobs[jobName]
	if !ok {
		t.Fatalf("missing job %q", jobName)
	}
	var commands string
	for _, step := range job.Steps {
		if step.Name == "Build binaries" {
			if commands != "" {
				t.Fatal("duplicate binary build step")
			}
			commands = step.Run
		}
	}
	if strings.TrimSpace(commands) == "" {
		t.Fatal("missing binary build commands")
	}
	return commands
}

func TestValidationBuildsTheReleaseBinaries(t *testing.T) {
	release := loadReleaseWorkflow(t, "release.yaml")
	validation := loadReleaseWorkflow(t, "fork-release-validation.yaml")
	if got, want := buildCommands(t, validation, "build-release"), buildCommands(t, release, "publish-binary"); got != want {
		t.Fatal("validation build differs from release.yaml; copy the release build commands exactly")
	}
	version := validation.Jobs["build-release"].Env["RELEASE_VERSION"]
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+-loft\.[0-9]+$`).MatchString(version) {
		t.Fatalf("validation version %q does not exercise the fork release version format", version)
	}
}

func TestValidationCannotPublish(t *testing.T) {
	workflow := loadReleaseWorkflow(t, "fork-release-validation.yaml")
	if !reflect.DeepEqual(workflow.Permissions, map[string]string{"contents": "read"}) {
		t.Fatalf("validation permissions = %v; want contents: read only", workflow.Permissions)
	}
	for name, job := range workflow.Jobs {
		for permission, value := range job.Permissions {
			if value != "read" && value != "none" {
				t.Errorf("job %s grants %s: %s", name, permission, value)
			}
		}
		for _, step := range job.Steps {
			if strings.Contains(step.Run, "gh release") || strings.Contains(step.Run, "git push") || strings.Contains(step.Uses, "actions/attest") {
				t.Errorf("job %s contains a release mutation", name)
			}
		}
	}
}

func TestPublicationRemainsTagOnlyAndLast(t *testing.T) {
	workflow := loadReleaseWorkflow(t, "release.yaml")
	if len(workflow.On) != 1 || workflow.On["push"] == nil {
		t.Fatal("release publication must be triggered only by a tag push")
	}
	push := workflow.On["push"].(map[string]any)
	if !reflect.DeepEqual(push, map[string]any{"tags": []any{"v*-loft.*"}}) {
		t.Fatalf("release trigger = %v; want fork tags only", push)
	}
	var ordered []string
	for _, step := range workflow.Jobs["publish-binary"].Steps {
		if step.Name != "" && step.Name != "Set up Go" {
			ordered = append(ordered, step.Name)
		}
	}
	want := []string{"Validate release tag name", "Build binaries", "Attest binaries", "Create draft release", "Upload binaries to draft release", "Publish release"}
	if !reflect.DeepEqual(ordered, want) {
		t.Fatalf("release order = %v; want %v", ordered, want)
	}
}
