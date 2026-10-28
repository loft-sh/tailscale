// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package tests

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type step struct {
	Name, Uses, Run string
	With            map[string]any
}
type job struct {
	Permissions map[string]string
	Steps       []step
}
type trigger struct {
	Branches       []string
	BranchesIgnore []string `yaml:"branches-ignore"`
	Paths          []string
}

func (tr *trigger) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	type plain trigger
	return node.Decode((*plain)(tr))
}

type workflow struct {
	On          map[string]trigger
	Permissions map[string]string
	Jobs        map[string]job
}

func read(t *testing.T, name string) workflow {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	var w workflow
	if err := yaml.Unmarshal(b, &w); err != nil {
		t.Fatal(err)
	}
	return w
}
func findStep(t *testing.T, j job, name string) step {
	t.Helper()
	for _, s := range j.Steps {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("missing step %q", name)
	return step{}
}
func setup(t *testing.T, j job) step {
	t.Helper()
	for _, s := range j.Steps {
		if strings.HasPrefix(s.Uses, "actions/setup-go@") {
			return s
		}
	}
	t.Fatal("missing setup-go")
	return step{}
}
func readOnly(t *testing.T, p map[string]string) {
	t.Helper()
	if p["contents"] != "read" {
		t.Errorf("contents permission must be read: %v", p)
	}
	for scope, level := range p {
		if level != "read" && level != "none" {
			t.Errorf("permission %s=%s is not read-only", scope, level)
		}
	}
}
func TestForkReleaseBuild(t *testing.T) {
	fork := read(t, "fork-release-validation.yaml")
	prod := read(t, "release.yaml")
	if len(fork.On) != 1 || !reflect.DeepEqual(fork.On["pull_request"].Branches, []string{"release-v*-loft.*"}) {
		t.Errorf("unexpected triggers: %+v", fork.On)
	}
	if len(fork.Jobs) != 2 {
		t.Errorf("unexpected jobs: %v", fork.Jobs)
	}
	readOnly(t, fork.Permissions)
	for name, j := range fork.Jobs {
		t.Run(name, func(t *testing.T) { readOnly(t, j.Permissions) })
	}
	build, ok := fork.Jobs["build-release"]
	if !ok {
		t.Fatal("missing build-release")
	}
	validation, ok := fork.Jobs["validate-workflow"]
	if !ok {
		t.Fatal("missing validate-workflow")
	}
	release, ok := prod.Jobs["publish-binary"]
	if !ok {
		t.Fatal("missing production job")
	}
	actual, expected := findStep(t, build, "Build binaries").Run, findStep(t, release, "Build binaries").Run
	if actual != expected {
		t.Error("validation build differs from production")
	}
	for _, arch := range []string{"amd64", "arm64"} {
		if !strings.Contains(actual, "GOARCH="+arch+" GOOS=linux go build") {
			t.Errorf("missing Linux %s build", arch)
		}
	}
	if strings.Contains(actual, "mkversion") || strings.Contains(actual, "-X ") {
		t.Error("build must not stamp a version")
	}
	a, b := setup(t, build).With, setup(t, release).With
	for _, key := range []string{"go-version", "go-version-file", "cache"} {
		if !reflect.DeepEqual(a[key], b[key]) {
			t.Errorf("setup-go %s differs: %v vs %v", key, a[key], b[key])
		}
	}
	// Allow only the actions and commands needed for validation; this rejects publish steps.
	for name, j := range map[string]job{"build-release": build, "validate-workflow": validation} {
		for _, s := range j.Steps {
			if s.Uses != "" && !strings.HasPrefix(s.Uses, "actions/checkout@") && !strings.HasPrefix(s.Uses, "actions/setup-go@") && !(name == "validate-workflow" && strings.HasPrefix(s.Uses, "zizmorcore/zizmor-action@")) {
				t.Errorf("unexpected action in %s: %s", name, s.Uses)
			}
			if s.Run != "" && !(name == "build-release" && s.Name == "Build binaries" && s.Run == expected) && !(name == "validate-workflow" && (s.Run == "go test ./.github/tests" || s.Run == "go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 .github/workflows/release.yaml .github/workflows/fork-release-validation.yaml")) {
				t.Errorf("unexpected command in %s: %q", name, s.Run)
			}
		}
	}
}

// GitHub applies branch patterns in order, with later negative patterns excluding matches.
func matches(patterns []string, branch string, initial bool) bool {
	result := initial
	for _, pattern := range patterns {
		negative := strings.HasPrefix(pattern, "!")
		pattern = strings.TrimPrefix(pattern, "!")
		matched, err := filepath.Match(pattern, branch)
		if err != nil {
			return false
		}
		if matched {
			result = !negative
		}
	}
	return result
}
func TestPullRequestRouting(t *testing.T) {
	forkPattern := "release-v*-loft.*"
	ignored := []string{"test.yml", "golangci-lint.yml", "checklocks.yml", "govulncheck.yml", "kubemanifests.yaml", "ssh-integrationtest.yml"}
	included := []string{"docker-file-build.yml", "installer.yml", "webclient.yml"}
	paths := map[string][]string{
		"checklocks.yml":          {"**/*.go", ".github/workflows/checklocks.yml"},
		"govulncheck.yml":         {".github/workflows/govulncheck.yml"},
		"kubemanifests.yaml":      {"cmd/k8s-operator/**", "k8s-operator/**", ".github/workflows/kubemanifests.yaml"},
		"ssh-integrationtest.yml": {"ssh/**", "tempfork/gliderlabs/ssh/**", ".github/workflows/ssh-integrationtest"},
		"installer.yml":           {"scripts/installer.sh"},
		"webclient.yml":           {"client/web/**", ".github/workflows/webclient.yml", "!**.md"},
	}
	cases := []struct {
		branch             string
		fork, star, codeql bool
	}{
		{"release-v1.78.1-loft.15", true, true, false}, {"release-v1.96.4-loft.3", true, true, false},
		{"main", false, true, true}, {"release-branch/1.78", false, false, false}, {"topic", false, true, false},
	}
	for _, file := range append(append(ignored, included...), "codeql-analysis.yml", "fork-release-validation.yaml") {
		t.Run(file, func(t *testing.T) {
			w := read(t, file)
			pr, ok := w.On["pull_request"]
			if !ok {
				t.Fatal("missing pull_request trigger")
			}
			if want, ok := paths[file]; ok && !reflect.DeepEqual(pr.Paths, want) {
				t.Errorf("path filters changed: %v", pr.Paths)
			}
			for _, c := range cases {
				want := !c.fork
				switch file {
				case "fork-release-validation.yaml":
					want = c.fork
				case "codeql-analysis.yml":
					want = c.codeql
				case "docker-file-build.yml", "installer.yml", "webclient.yml":
					want = c.star && !c.fork
				}
				got := true
				if len(pr.Branches) > 0 {
					got = matches(pr.Branches, c.branch, false)
				}
				if len(pr.BranchesIgnore) > 0 {
					got = got && !matches(pr.BranchesIgnore, c.branch, false)
				}
				if got != want {
					t.Errorf("base %s: got %v want %v", c.branch, got, want)
				}
			}
			for _, f := range ignored {
				if file == f && (!reflect.DeepEqual(pr.BranchesIgnore, []string{forkPattern}) || len(pr.Branches) != 0) {
					t.Errorf("exclusion widened or changed: %+v", pr)
				}
			}
			for _, f := range included {
				if file == f && (!reflect.DeepEqual(pr.Branches, []string{"*", "!" + forkPattern}) || len(pr.BranchesIgnore) != 0) {
					t.Errorf("branch filter changed: %+v", pr)
				}
			}
			if file == "codeql-analysis.yml" && !reflect.DeepEqual(pr.Branches, []string{"main"}) {
				t.Errorf("CodeQL filter changed: %+v", pr)
			}
		})
	}
}
