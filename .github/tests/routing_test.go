// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package workflows

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

type routingWorkflow struct {
	On map[string]yaml.Node `yaml:"on"`
}

type routingTrigger struct {
	Branches       []string `yaml:"branches"`
	BranchesIgnore []string `yaml:"branches-ignore"`
}

func decodeRouting(t *testing.T, node yaml.Node) routingTrigger {
	t.Helper()
	var trigger routingTrigger
	if node.Tag != "!!null" {
		if err := node.Decode(&trigger); err != nil {
			t.Fatal(err)
		}
	}
	return trigger
}

func routingAccepts(t *testing.T, includes, excludes []string, branch string) bool {
	t.Helper()
	match := func(patterns []string) bool {
		for _, pattern := range patterns {
			matched, err := filepath.Match(pattern, branch)
			if err != nil {
				t.Fatalf("invalid branch pattern %q: %v", pattern, err)
			}
			if matched {
				return true
			}
		}
		return false
	}
	return (len(includes) == 0 || match(includes)) && !match(excludes)
}

func TestPullRequestRouting(t *testing.T) {
	paths, err := filepath.Glob("../workflows/*.y*ml")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no workflow files found")
	}
	seenValidation, inherited := false, 0
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var workflow routingWorkflow
		if err := yaml.Unmarshal(data, &workflow); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		prNode, ok := workflow.On["pull_request"]
		if !ok {
			continue
		}
		pr := decodeRouting(t, prNode)
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			accept := func(branch string) bool { return routingAccepts(t, pr.Branches, pr.BranchesIgnore, branch) }
			if name == "fork-release-validation.yaml" {
				seenValidation = true
				for _, branch := range []string{"release-v1.96.4-loft.3", "release-v1.78.0-loft.1"} {
					if !accept(branch) {
						t.Errorf("validation excludes %s", branch)
					}
				}
				for _, branch := range []string{"main", "feature/example"} {
					if accept(branch) {
						t.Errorf("validation accepts %s", branch)
					}
				}
				return
			}
			inherited++
			for _, branch := range []string{"release-v1.96.4-loft.3", "release-v1.78.0-loft.1"} {
				if accept(branch) {
					t.Errorf("inherited workflow accepts %s", branch)
				}
			}
			if !accept("main") {
				t.Error("inherited workflow excludes main")
			}
			mainOnly := name == "codeql-analysis.yml" || name == "pin-github-actions.yml"
			if accept("release-branch/1.96") == mainOnly {
				t.Error("upstream release branch routing changed")
			}
		})
	}
	if inherited == 0 {
		t.Error("no inherited pull request workflows found")
	}
	if !seenValidation {
		t.Error("fork release validation workflow missing")
	}
}

func TestMainCINonPRTriggers(t *testing.T) {
	data, err := os.ReadFile("../workflows/test.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow routingWorkflow
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"push", "merge_group"} {
		node, ok := workflow.On[event]
		trigger := decodeRouting(t, node)
		if !ok || !routingAccepts(t, trigger.Branches, trigger.BranchesIgnore, "main") {
			t.Errorf("%s no longer targets main", event)
		}
	}
	push := decodeRouting(t, workflow.On["push"])
	if !routingAccepts(t, push.Branches, push.BranchesIgnore, "release-branch/1.96") {
		t.Error("push no longer targets upstream release branch")
	}
}
