// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type recordingUpdateRunner struct {
	outputs map[string]string
	calls   []string
}

func (r *recordingUpdateRunner) Run(dir string, name string, args ...string) (string, error) {
	call := strings.Join(append([]string{name}, args...), " ")
	r.calls = append(r.calls, call)
	if output, ok := r.outputs[call]; ok {
		return output, nil
	}
	return "", nil
}

func (r *recordingUpdateRunner) calledContains(substr string) bool {
	for _, call := range r.calls {
		if strings.Contains(call, substr) {
			return true
		}
	}
	return false
}

func testUpdateContext(r *recordingUpdateRunner, stdout *bytes.Buffer) updateContext {
	return updateContext{
		RepoDir:         "/repo/waveterm",
		SourceRepo:      "/repo/source-waveterm",
		SourceRef:       "main",
		ActiveWshPath:   "/active/bin/wsh",
		ActiveWavePath:  "/active/bin/wave",
		ActiveAppPath:   "/Applications/Wave.app",
		PackagedAppPath: "/repo/waveterm/dist/mac-arm64/Wave.app",
		UpdateStatePath: "",
		Version:         "0.14.5",
		Runner:          r,
		Stdout:          stdout,
		Now: func() time.Time {
			return time.Date(2026, 5, 4, 12, 34, 0, 0, time.UTC)
		},
		SetupCommand:    []string{"/active/bin/wsh", "rcfiles"},
		InstalledCommit: "abcdef1234567890",
	}
}

func TestRunUpdateCheckOnlyFetchesAndCompares(t *testing.T) {
	runner := &recordingUpdateRunner{outputs: map[string]string{
		"git rev-list --count HEAD..FETCH_HEAD": "2\n",
		"git rev-parse HEAD":                    "abcdef1234567890\n",
	}}
	var stdout bytes.Buffer

	err := runUpdate(updateOptions{Check: true}, testUpdateContext(runner, &stdout))
	if err != nil {
		t.Fatalf("runUpdate() error = %v", err)
	}

	if !runner.calledContains("git fetch --quiet /repo/source-waveterm main") {
		t.Fatalf("expected check to fetch source repo, calls: %#v", runner.calls)
	}
	if !runner.calledContains("git rev-list --count HEAD..FETCH_HEAD") {
		t.Fatalf("expected check to compare HEAD with FETCH_HEAD, calls: %#v", runner.calls)
	}
	if runner.calledContains("go build") || runner.calledContains("git merge") || runner.calledContains("npm install") || runner.calledContains("electron-builder") {
		t.Fatalf("check must not build, package, install, or merge, calls: %#v", runner.calls)
	}
	if !strings.Contains(stdout.String(), "2 source commits available") {
		t.Fatalf("expected available update count in stdout, got %q", stdout.String())
	}
}

func TestRunUpdateCheckReportsUnknownAppInstallState(t *testing.T) {
	runner := &recordingUpdateRunner{outputs: map[string]string{
		"git rev-list --count HEAD..FETCH_HEAD": "0\n",
		"git rev-parse HEAD":                    "abcdef1234567890\n",
	}}
	var stdout bytes.Buffer
	ctx := testUpdateContext(runner, &stdout)
	ctx.InstalledCommit = ""

	err := runUpdate(updateOptions{Check: true}, ctx)
	if err != nil {
		t.Fatalf("runUpdate() error = %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "Wave app install state is unknown") || !strings.Contains(out, "Run: wave update") {
		t.Fatalf("expected unknown app state to request update, got %q", out)
	}
	if runner.calledContains("electron-builder") || runner.calledContains("ditto") {
		t.Fatalf("check must not package/install app, calls: %#v", runner.calls)
	}
}

func TestRunUpdateCheckReportsStaleAppPackage(t *testing.T) {
	runner := &recordingUpdateRunner{outputs: map[string]string{
		"git rev-list --count HEAD..FETCH_HEAD": "0\n",
		"git rev-parse HEAD":                    "abcdef1234567890\n",
	}}
	var stdout bytes.Buffer
	ctx := testUpdateContext(runner, &stdout)
	ctx.InstalledCommit = "1111111111111111"

	err := runUpdate(updateOptions{Check: true}, ctx)
	if err != nil {
		t.Fatalf("runUpdate() error = %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "Wave app package is stale") || !strings.Contains(out, "Run: wave update") {
		t.Fatalf("expected stale app package warning, got %q", out)
	}
}

func TestRunUpdateCheckReportsAppAndWshUpToDate(t *testing.T) {
	runner := &recordingUpdateRunner{outputs: map[string]string{
		"git rev-list --count HEAD..FETCH_HEAD": "0\n",
		"git rev-parse HEAD":                    "abcdef1234567890\n",
	}}
	var stdout bytes.Buffer

	err := runUpdate(updateOptions{Check: true}, testUpdateContext(runner, &stdout))
	if err != nil {
		t.Fatalf("runUpdate() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "Wave app, wave, and wsh are up to date") {
		t.Fatalf("expected up-to-date output, got %q", stdout.String())
	}
}

func TestRunUpdateSimplePackagesAppAndInstallsActiveWsh(t *testing.T) {
	runner := &recordingUpdateRunner{outputs: map[string]string{
		"git status --porcelain":               "",
		"git diff --name-only HEAD FETCH_HEAD": "frontend/app/view/term/term.tsx\n",
		"git rev-parse HEAD":                   "abcdef1234567890\n",
	}}
	var stdout bytes.Buffer

	err := runUpdate(updateOptions{Simple: true}, testUpdateContext(runner, &stdout))
	if err != nil {
		t.Fatalf("runUpdate() error = %v", err)
	}

	wantCalls := []string{
		"git status --porcelain",
		"git fetch --quiet /repo/source-waveterm main",
		"git diff --name-only HEAD FETCH_HEAD",
		"git merge --ff-only FETCH_HEAD",
		"npm run build:prod",
		"env CSC_IDENTITY_AUTO_DISCOVERY=false npm exec electron-builder -- -c electron-builder.config.cjs -p never --dir",
		"rm -rf /Applications/Wave.app",
		"ditto /repo/waveterm/dist/mac-arm64/Wave.app /Applications/Wave.app",
		"env GOTOOLCHAIN=go1.26.2 go build -ldflags=-s -w -X main.BuildTime=202605041234 -X main.WaveVersion=0.14.5 -o /active/bin/wave cmd/wsh/main-wsh.go",
		"env GOTOOLCHAIN=go1.26.2 go build -ldflags=-s -w -X main.BuildTime=202605041234 -X main.WaveVersion=0.14.5 -o /active/bin/wsh cmd/wsh/main-wsh.go",
		"git rev-parse HEAD",
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls mismatch\n got: %#v\nwant: %#v", runner.calls, wantCalls)
	}
	out := stdout.String()
	if !strings.Contains(out, "Wave app updated") || !strings.Contains(out, "wave updated") || !strings.Contains(out, "wsh updated") {
		t.Fatalf("expected app and wsh success output, got %q", out)
	}
}

func TestRunUpdateSimpleWarnsWhenDependencyFilesChanged(t *testing.T) {
	runner := &recordingUpdateRunner{outputs: map[string]string{
		"git status --porcelain":               "",
		"git diff --name-only HEAD FETCH_HEAD": "go.mod\ncmd/wsh/main-wsh.go\n",
		"git rev-parse HEAD":                   "abcdef1234567890\n",
	}}
	var stdout bytes.Buffer

	err := runUpdate(updateOptions{Simple: true}, testUpdateContext(runner, &stdout))
	if err != nil {
		t.Fatalf("runUpdate() error = %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "Run: wave update") || !strings.Contains(out, "dependencies may need reinstall") {
		t.Fatalf("expected full update dependency reminder, got %q", out)
	}
	if runner.calledContains("npm install") {
		t.Fatalf("simple update must not run full dependency install, calls: %#v", runner.calls)
	}
}

func TestRunUpdateSimpleWarnsWhenSetupSensitiveFilesChanged(t *testing.T) {
	runner := &recordingUpdateRunner{outputs: map[string]string{
		"git status --porcelain":               "",
		"git diff --name-only HEAD FETCH_HEAD": "pkg/wshutil/wshutil.go\n",
		"git rev-parse HEAD":                   "abcdef1234567890\n",
	}}
	var stdout bytes.Buffer

	err := runUpdate(updateOptions{Simple: true}, testUpdateContext(runner, &stdout))
	if err != nil {
		t.Fatalf("runUpdate() error = %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "Run: wave update --setup") || !strings.Contains(out, "setup/rcfile integration may need refresh") {
		t.Fatalf("expected setup reminder, got %q", out)
	}
	if runner.calledContains("/active/bin/wsh rcfiles") {
		t.Fatalf("simple update must not run setup, calls: %#v", runner.calls)
	}
}

func TestRunUpdateSetupRunsRcfilesAfterFullUpdate(t *testing.T) {
	runner := &recordingUpdateRunner{outputs: map[string]string{
		"git status --porcelain":               "",
		"git diff --name-only HEAD FETCH_HEAD": "pkg/wshutil/wshutil.go\n",
		"git rev-parse HEAD":                   "abcdef1234567890\n",
	}}
	var stdout bytes.Buffer

	err := runUpdate(updateOptions{Setup: true}, testUpdateContext(runner, &stdout))
	if err != nil {
		t.Fatalf("runUpdate() error = %v", err)
	}

	if !runner.calledContains("npm install") {
		t.Fatalf("full setup update should install dependencies, calls: %#v", runner.calls)
	}
	if !runner.calledContains("env GOTOOLCHAIN=go1.26.2 go mod tidy") {
		t.Fatalf("full setup update should pin usable Go toolchain, calls: %#v", runner.calls)
	}
	if !runner.calledContains("npm run build:prod") || !runner.calledContains("electron-builder") || !runner.calledContains("ditto /repo/waveterm/dist/mac-arm64/Wave.app /Applications/Wave.app") {
		t.Fatalf("full setup update should package/install app, calls: %#v", runner.calls)
	}
	if !runner.calledContains("/active/bin/wsh rcfiles") {
		t.Fatalf("setup update should run rcfile setup after install, calls: %#v", runner.calls)
	}
}

func TestRunUpdateFullPrintsLongRunningProgress(t *testing.T) {
	runner := &recordingUpdateRunner{outputs: map[string]string{
		"git status --porcelain":               "",
		"git diff --name-only HEAD FETCH_HEAD": "frontend/app/view/term/term.tsx\n",
		"git rev-parse HEAD":                   "abcdef1234567890\n",
	}}
	var stdout bytes.Buffer

	err := runUpdate(updateOptions{}, testUpdateContext(runner, &stdout))
	if err != nil {
		t.Fatalf("runUpdate() error = %v", err)
	}

	out := stdout.String()
	want := []string{
		"Checking worktree",
		"Fetching updates",
		"Installing dependencies",
		"Generating schema/types",
		"Packaging Wave app",
		"Installing Wave app",
		"Installing wave",
		"Installing wsh",
	}
	for _, msg := range want {
		if !strings.Contains(out, msg) {
			t.Fatalf("expected progress message %q in output: %q", msg, out)
		}
	}
}

func TestDefaultPackagedWaveAppPathPrefersElectronBuilderMakeDir(t *testing.T) {
	repoDir := t.TempDir()
	makeApp := filepath.Join(repoDir, "make", "mac-arm64", "Wave.app")
	if err := os.MkdirAll(makeApp, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := defaultPackagedWaveAppPath(repoDir); got != makeApp {
		t.Fatalf("defaultPackagedWaveAppPath() = %q, want %q", got, makeApp)
	}
}

func TestDefaultActiveWshPathForWaveExecutableUsesSiblingWsh(t *testing.T) {
	got, ok := defaultActiveWshPathForExecutable("/active/bin/wave")
	if !ok {
		t.Fatal("expected wave executable to infer sibling wsh path")
	}
	if got != "/active/bin/wsh" {
		t.Fatalf("defaultActiveWshPathForExecutable() = %q, want /active/bin/wsh", got)
	}
}

func TestUpdateCommandNameDefaultsToWave(t *testing.T) {
	if got := updateCommandName(); got != "wave" {
		t.Fatalf("updateCommandName() = %q, want wave", got)
	}
}

func TestRunUpdateRefusesDirtyWorktree(t *testing.T) {
	runner := &recordingUpdateRunner{outputs: map[string]string{
		"git status --porcelain": " M cmd/wsh/main-wsh.go\n",
	}}
	var stdout bytes.Buffer

	err := runUpdate(updateOptions{Simple: true}, testUpdateContext(runner, &stdout))
	if err == nil {
		t.Fatal("expected dirty worktree error")
	}
	if !errors.Is(err, errUpdateDirtyWorktree) {
		t.Fatalf("expected errUpdateDirtyWorktree, got %v", err)
	}
	if !strings.Contains(err.Error(), "wave update") {
		t.Fatalf("dirty worktree error should mention wave update, got %v", err)
	}
	if runner.calledContains("git fetch") || runner.calledContains("go build") || runner.calledContains("electron-builder") {
		t.Fatalf("dirty worktree must stop before fetch/build/package, calls: %#v", runner.calls)
	}
}
