// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/wavetermdev/waveterm/pkg/wavebase"
)

var (
	updateCheck  bool
	updateSimple bool
	updateSetup  bool
	updateYes    bool
)

var errUpdateDirtyWorktree = errors.New("dirty worktree")

type updateOptions struct {
	Check  bool
	Simple bool
	Setup  bool
	Yes    bool
}

type updateRunner interface {
	Run(dir string, name string, args ...string) (string, error)
}

type updateStreamingRunner interface {
	RunStreaming(dir string, name string, args ...string) (string, error)
}

type execUpdateRunner struct{}

type updateContext struct {
	RepoDir         string
	SourceRepo      string
	SourceRef       string
	ActiveWshPath   string
	ActiveWavePath  string
	ActiveAppPath   string
	PackagedAppPath string
	UpdateStatePath string
	Version         string
	Runner          updateRunner
	Stdout          io.Writer
	Now             func() time.Time
	SetupCommand    []string
	InstalledCommit string
}

var updateCmd = &cobra.Command{
	Use:   "update [--check|--simple|--setup]",
	Short: "Update the local Wave app development install",
	Long: `Update the local Wave app development install from a source checkout or remote.

The default mode fetches updates, installs dependencies, regenerates generated files,
packages the Electron app, installs the app bundle, and refreshes the active wave/wsh binaries.
--simple is a developer fast path: it skips dependency installation and generation, but
still rebuilds/packages/installs the Wave app. If --simple sees dependency- or
setup-sensitive files changed, it prints explicit follow-up commands to run.
--check fetches and compares only; it never builds, packages, or installs.`,
	RunE:                  runUpdateCmd,
	DisableFlagsInUseLine: true,
}

func init() {
	updateCmd.Flags().BoolVar(&updateCheck, "check", false, "fetch and report whether source/app updates are available without building or installing")
	updateCmd.Flags().BoolVar(&updateSimple, "simple", false, "developer fast path: fast-forward, rebuild, package, and install Wave without dependency install or setup")
	updateCmd.Flags().BoolVar(&updateSetup, "setup", false, "run full update and refresh wsh rcfile setup after installing")
	updateCmd.Flags().BoolVar(&updateYes, "yes", false, "assume yes for non-interactive update steps")
	rootCmd.AddCommand(updateCmd)
}

func runUpdateCmd(cmd *cobra.Command, args []string) (rtnErr error) {
	defer func() {
		sendActivity("update", rtnErr == nil)
	}()
	if updateCheck && (updateSimple || updateSetup) {
		return fmt.Errorf("--check cannot be combined with --simple or --setup")
	}
	if updateSimple && updateSetup {
		return fmt.Errorf("--simple cannot be combined with --setup")
	}
	ctx, err := defaultUpdateContext()
	if err != nil {
		return err
	}
	return runUpdate(updateOptions{Check: updateCheck, Simple: updateSimple, Setup: updateSetup, Yes: updateYes}, ctx)
}

func (execUpdateRunner) Run(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s failed: %w\n%s", name, strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

func (execUpdateRunner) RunStreaming(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var output bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &output)
	cmd.Stderr = io.MultiWriter(os.Stderr, &output)
	label := strings.TrimSpace(name + " " + strings.Join(args, " "))
	fmt.Fprintf(os.Stderr, "[wsh update] running: %s\n", label)
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return output.String(), err
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			if err != nil {
				return output.String(), fmt.Errorf("%s failed after %s: %w\n%s", label, time.Since(start).Round(time.Second), err, output.String())
			}
			fmt.Fprintf(os.Stderr, "[wsh update] finished: %s (%s)\n", label, time.Since(start).Round(time.Second))
			return output.String(), nil
		case <-ticker.C:
			fmt.Fprintf(os.Stderr, "[wsh update] still running: %s (%s elapsed)\n", label, time.Since(start).Round(time.Second))
		}
	}
}

func defaultUpdateContext() (updateContext, error) {
	runner := execUpdateRunner{}
	repoDir, err := defaultUpdateRepoDir(runner)
	if err != nil {
		return updateContext{}, err
	}
	activeWshPath, err := defaultActiveWshPath()
	if err != nil {
		return updateContext{}, err
	}
	activeWavePath := defaultActiveWavePath(activeWshPath)
	activeAppPath, err := defaultActiveWaveAppPath()
	if err != nil {
		return updateContext{}, err
	}
	version := strings.TrimSpace(wavebase.WaveVersion)
	if out, err := runner.Run(repoDir, "node", "version.cjs"); err == nil && strings.TrimSpace(out) != "" {
		version = strings.TrimSpace(out)
	}
	if version == "" {
		version = "0.0.0"
	}
	statePath := defaultUpdateStatePath()
	return updateContext{
		RepoDir:         repoDir,
		SourceRepo:      defaultUpdateSourceRepo(repoDir),
		SourceRef:       defaultUpdateSourceRef(),
		ActiveWshPath:   activeWshPath,
		ActiveWavePath:  activeWavePath,
		ActiveAppPath:   activeAppPath,
		PackagedAppPath: defaultPackagedWaveAppPath(repoDir),
		UpdateStatePath: statePath,
		Version:         version,
		Runner:          runner,
		Stdout:          WrappedStdout,
		Now:             time.Now,
		SetupCommand:    []string{activeWshPath, "rcfiles"},
		InstalledCommit: readUpdateStateCommit(statePath),
	}, nil
}

func defaultUpdateRepoDir(runner updateRunner) (string, error) {
	if envRepo := os.Getenv("WAVETERM_UPDATE_TARGET_REPO"); envRepo != "" {
		return filepath.Abs(envRepo)
	}
	if cwd, err := os.Getwd(); err == nil {
		if out, err := runner.Run(cwd, "git", "rev-parse", "--show-toplevel"); err == nil && strings.TrimSpace(out) != "" {
			return strings.TrimSpace(out), nil
		}
	}
	fallback := "/Users/rain/dev/-github/waveterm"
	if isDir(fallback) {
		return fallback, nil
	}
	return "", fmt.Errorf("could not determine waveterm source checkout; set WAVETERM_UPDATE_TARGET_REPO")
}

func defaultUpdateSourceRepo(repoDir string) string {
	if envSource := os.Getenv("WAVETERM_UPDATE_SOURCE_REPO"); envSource != "" {
		return envSource
	}
	base := filepath.Base(repoDir)
	if strings.HasSuffix(base, "-update") {
		sibling := filepath.Join(filepath.Dir(repoDir), strings.TrimSuffix(base, "-update"))
		if isDir(sibling) {
			return sibling
		}
	}
	localMain := "/Users/rain/dev/-github/waveterm"
	if isDir(localMain) && filepath.Clean(localMain) != filepath.Clean(repoDir) {
		return localMain
	}
	return "origin"
}

func defaultUpdateSourceRef() string {
	if envRef := os.Getenv("WAVETERM_UPDATE_SOURCE_REF"); envRef != "" {
		return envRef
	}
	return "main"
}

func defaultActiveWshPath() (string, error) {
	if envPath := os.Getenv("WAVETERM_UPDATE_ACTIVE_WSH"); envPath != "" {
		return envPath, nil
	}
	path, err := exec.LookPath("wsh")
	if err != nil {
		return "", fmt.Errorf("could not find active wsh on PATH; set WAVETERM_UPDATE_ACTIVE_WSH")
	}
	if realPath, err := filepath.EvalSymlinks(path); err == nil {
		return realPath, nil
	}
	return path, nil
}

func defaultActiveWavePath(activeWshPath string) string {
	if envPath := os.Getenv("WAVETERM_UPDATE_ACTIVE_WAVE"); envPath != "" {
		return envPath
	}
	if path, err := exec.LookPath("wave"); err == nil {
		if realPath, err := filepath.EvalSymlinks(path); err == nil {
			return realPath
		}
		return path
	}
	if activeWshPath != "" {
		return filepath.Join(filepath.Dir(activeWshPath), "wave")
	}
	return "wave"
}

func defaultActiveWaveAppPath() (string, error) {
	if envPath := os.Getenv("WAVETERM_UPDATE_ACTIVE_APP"); envPath != "" {
		return envPath, nil
	}
	candidates := []string{
		"/Applications/Wave.app",
		filepath.Join(os.Getenv("HOME"), "Applications", "Wave.app"),
	}
	for _, candidate := range candidates {
		if isDir(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not find active Wave.app; set WAVETERM_UPDATE_ACTIVE_APP")
}

func defaultPackagedWaveAppPath(repoDir string) string {
	if envPath := os.Getenv("WAVETERM_UPDATE_PACKAGED_APP"); envPath != "" {
		return envPath
	}
	candidates := []string{
		filepath.Join(repoDir, "dist", "mac-arm64", "Wave.app"),
		filepath.Join(repoDir, "dist", "mac", "Wave.app"),
		filepath.Join(repoDir, "dist", "mac-universal", "Wave.app"),
	}
	for _, candidate := range candidates {
		if isDir(candidate) {
			return candidate
		}
	}
	return candidates[0]
}

func defaultUpdateStatePath() string {
	if envPath := os.Getenv("WAVETERM_UPDATE_STATE_FILE"); envPath != "" {
		return envPath
	}
	home := os.Getenv("HOME")
	if runtime.GOOS == "darwin" && home != "" {
		return filepath.Join(home, "Library", "Application Support", "waveterm", "local-update-head")
	}
	if configDir, err := os.UserConfigDir(); err == nil && configDir != "" {
		return filepath.Join(configDir, "waveterm", "local-update-head")
	}
	return ""
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func runUpdate(opts updateOptions, ctx updateContext) error {
	if ctx.Runner == nil {
		ctx.Runner = execUpdateRunner{}
	}
	if ctx.Stdout == nil {
		ctx.Stdout = WrappedStdout
	}
	if ctx.Now == nil {
		ctx.Now = time.Now
	}
	if ctx.SourceRef == "" {
		ctx.SourceRef = "main"
	}
	if opts.Check {
		return runUpdateCheck(ctx)
	}
	announceUpdateStep(ctx, "Checking worktree", ctx.RepoDir)
	if err := ensureUpdateWorktreeClean(ctx); err != nil {
		return err
	}
	announceUpdateStep(ctx, "Fetching updates", fmt.Sprintf("%s %s", ctx.SourceRepo, ctx.SourceRef))
	if err := fetchUpdateSource(ctx); err != nil {
		return err
	}
	changedOutput, err := ctx.Runner.Run(ctx.RepoDir, "git", "diff", "--name-only", "HEAD", "FETCH_HEAD")
	if err != nil {
		return err
	}
	changedFiles := parseUpdateChangedFiles(changedOutput)
	announceUpdateStep(ctx, "Fast-forwarding checkout", ctx.RepoDir)
	if _, err := ctx.Runner.Run(ctx.RepoDir, "git", "merge", "--ff-only", "FETCH_HEAD"); err != nil {
		return err
	}
	if !opts.Simple {
		if err := runFullUpdatePrep(ctx); err != nil {
			return err
		}
	}
	if err := packageAndInstallWaveApp(ctx); err != nil {
		return err
	}
	if err := buildAndInstallActiveWave(ctx); err != nil {
		return err
	}
	if err := buildAndInstallActiveWsh(ctx); err != nil {
		return err
	}
	if opts.Setup {
		if err := runUpdateSetup(ctx); err != nil {
			return err
		}
	}
	if err := recordInstalledCommit(ctx); err != nil {
		return err
	}
	printSimpleUpdateReminders(ctx, opts, changedFiles)
	fmt.Fprintf(ctx.Stdout, "Wave app updated: %s\n", ctx.ActiveAppPath)
	fmt.Fprintf(ctx.Stdout, "wave updated: %s\n", ctx.ActiveWavePath)
	fmt.Fprintf(ctx.Stdout, "wsh updated: %s\n", ctx.ActiveWshPath)
	return nil
}

func runUpdateCheck(ctx updateContext) error {
	announceUpdateStep(ctx, "Fetching updates", fmt.Sprintf("%s %s", ctx.SourceRepo, ctx.SourceRef))
	if err := fetchUpdateSource(ctx); err != nil {
		return err
	}
	count, err := ctx.Runner.Run(ctx.RepoDir, "git", "rev-list", "--count", "HEAD..FETCH_HEAD")
	if err != nil {
		return err
	}
	count = strings.TrimSpace(count)
	currentCommit, err := currentUpdateCommit(ctx)
	if err != nil {
		return err
	}
	installedCommit := strings.TrimSpace(ctx.InstalledCommit)
	if count != "" && count != "0" {
		fmt.Fprintf(ctx.Stdout, "%s source commits available\n", count)
		return nil
	}
	if installedCommit == "" {
		fmt.Fprintf(ctx.Stdout, "Wave app install state is unknown for %s\nRun: %s update\n", ctx.ActiveAppPath, updateCommandName())
		return nil
	}
	if installedCommit != currentCommit {
		fmt.Fprintf(ctx.Stdout, "Wave app package is stale: installed %s, checkout %s\nRun: %s update\n", shortUpdateCommit(installedCommit), shortUpdateCommit(currentCommit), updateCommandName())
		return nil
	}
	fmt.Fprintf(ctx.Stdout, "Wave app, wave, and wsh are up to date\n")
	return nil
}

func announceUpdateStep(ctx updateContext, label string, detail string) {
	if detail == "" {
		fmt.Fprintf(ctx.Stdout, "==> %s\n", label)
		return
	}
	fmt.Fprintf(ctx.Stdout, "==> %s: %s\n", label, detail)
}

func ensureUpdateWorktreeClean(ctx updateContext) error {
	out, err := ctx.Runner.Run(ctx.RepoDir, "git", "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) == "" {
		return nil
	}
	return fmt.Errorf("%w: %s has local changes; commit or stash before running wsh update", errUpdateDirtyWorktree, ctx.RepoDir)
}

func fetchUpdateSource(ctx updateContext) error {
	source := ctx.SourceRepo
	if source == "" {
		source = "origin"
	}
	_, err := ctx.Runner.Run(ctx.RepoDir, "git", "fetch", "--quiet", source, ctx.SourceRef)
	return err
}

func currentUpdateCommit(ctx updateContext) (string, error) {
	out, err := ctx.Runner.Run(ctx.RepoDir, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func parseUpdateChangedFiles(output string) []string {
	var files []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files
}

func runUpdateCommand(ctx updateContext, command []string) (string, error) {
	if len(command) == 0 {
		return "", nil
	}
	if streamingRunner, ok := ctx.Runner.(updateStreamingRunner); ok {
		return streamingRunner.RunStreaming(ctx.RepoDir, command[0], command[1:]...)
	}
	return ctx.Runner.Run(ctx.RepoDir, command[0], command[1:]...)
}

func runFullUpdatePrep(ctx updateContext) error {
	commands := []struct {
		Label   string
		Command []string
	}{
		{Label: "Installing dependencies", Command: []string{"npm", "install", "--foreground-scripts"}},
		{Label: "Tidying Go modules", Command: []string{"go", "mod", "tidy"}},
		{Label: "Generating schema/types", Command: []string{"go", "run", "cmd/generateschema/main-generateschema.go"}},
		{Label: "Generating TypeScript types", Command: []string{"go", "run", "cmd/generatets/main-generatets.go"}},
		{Label: "Generating Go constants", Command: []string{"go", "run", "cmd/generatego/main-generatego.go"}},
	}
	for _, command := range commands {
		announceUpdateStep(ctx, command.Label, strings.Join(command.Command, " "))
		if _, err := runUpdateCommand(ctx, command.Command); err != nil {
			return err
		}
	}
	return nil
}

func packageAndInstallWaveApp(ctx updateContext) error {
	if err := packageWaveApp(ctx); err != nil {
		return err
	}
	return installPackagedWaveApp(ctx)
}

func packageWaveApp(ctx updateContext) error {
	commands := []struct {
		Label   string
		Command []string
	}{
		{Label: "Building frontend/backend bundle", Command: []string{"npm", "run", "build:prod"}},
		{Label: "Packaging Wave app", Command: []string{"npm", "exec", "electron-builder", "--", "-c", "electron-builder.config.cjs", "-p", "never", "--dir"}},
	}
	for _, command := range commands {
		announceUpdateStep(ctx, command.Label, strings.Join(command.Command, " "))
		if _, err := runUpdateCommand(ctx, command.Command); err != nil {
			return err
		}
	}
	return nil
}

func installPackagedWaveApp(ctx updateContext) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("automatic Wave app bundle install is currently supported only on macOS")
	}
	packagedApp := ctx.PackagedAppPath
	if packagedApp == "" {
		packagedApp = defaultPackagedWaveAppPath(ctx.RepoDir)
	}
	if packagedApp == "" {
		return fmt.Errorf("could not determine packaged Wave.app path")
	}
	announceUpdateStep(ctx, "Installing Wave app", fmt.Sprintf("%s -> %s", packagedApp, ctx.ActiveAppPath))
	if _, err := runUpdateCommand(ctx, []string{"rm", "-rf", ctx.ActiveAppPath}); err != nil {
		return err
	}
	_, err := runUpdateCommand(ctx, []string{"ditto", packagedApp, ctx.ActiveAppPath})
	return err
}

func buildAndInstallActiveWave(ctx updateContext) error {
	return buildAndInstallCommandBinary(ctx, "Installing wave", ctx.ActiveWavePath)
}

func buildAndInstallActiveWsh(ctx updateContext) error {
	return buildAndInstallCommandBinary(ctx, "Installing wsh", ctx.ActiveWshPath)
}

func buildAndInstallCommandBinary(ctx updateContext, label string, outPath string) error {
	buildTime := ctx.Now().Format("200601021504")
	ldflags := fmt.Sprintf("-s -w -X main.BuildTime=%s -X main.WaveVersion=%s", buildTime, ctx.Version)
	announceUpdateStep(ctx, label, outPath)
	_, err := runUpdateCommand(ctx, []string{"go", "build", "-ldflags=" + ldflags, "-o", outPath, "cmd/wsh/main-wsh.go"})
	return err
}

func runUpdateSetup(ctx updateContext) error {
	if len(ctx.SetupCommand) == 0 {
		fmt.Fprintf(ctx.Stdout, "No setup step is defined for wsh update.\n")
		return nil
	}
	announceUpdateStep(ctx, "Refreshing wsh rcfiles", strings.Join(ctx.SetupCommand, " "))
	_, err := runUpdateCommand(ctx, ctx.SetupCommand)
	return err
}

func printSimpleUpdateReminders(ctx updateContext, opts updateOptions, changedFiles []string) {
	if !opts.Simple {
		return
	}
	if hasUpdateDependencyChanges(changedFiles) {
		fmt.Fprintf(ctx.Stdout, "Run: %s update\nReason: dependencies may need reinstall before rebuild/runtime use.\n", updateCommandName())
	}
	if hasUpdateSetupSensitiveChanges(changedFiles) {
		fmt.Fprintf(ctx.Stdout, "Run: %s update --setup\nReason: setup/rcfile integration may need refresh.\n", updateCommandName())
	}
}

func hasUpdateDependencyChanges(files []string) bool {
	for _, file := range files {
		base := filepath.Base(file)
		if base == "go.mod" || base == "go.sum" || base == "package.json" || base == "package-lock.json" || base == "npm-shrinkwrap.json" || base == "pnpm-lock.yaml" || base == "yarn.lock" {
			return true
		}
	}
	return false
}

func hasUpdateSetupSensitiveChanges(files []string) bool {
	for _, file := range files {
		clean := filepath.ToSlash(file)
		if clean == "Taskfile.yml" || clean == "cmd/wsh/cmd/wshcmd-rcfiles.go" || clean == "cmd/wsh/main-wsh.go" {
			return true
		}
		if strings.HasPrefix(clean, "pkg/wshutil/") || strings.HasPrefix(clean, "pkg/util/shellutil/") || strings.Contains(clean, "completion") {
			return true
		}
		if runtime.GOOS == "darwin" && strings.Contains(strings.ToLower(clean), "launchagent") {
			return true
		}
	}
	return false
}

func updateCommandName() string {
	return "wave"
}

func readUpdateStateCommit(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func recordInstalledCommit(ctx updateContext) error {
	commit, err := currentUpdateCommit(ctx)
	if err != nil {
		return err
	}
	if ctx.UpdateStatePath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(ctx.UpdateStatePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(ctx.UpdateStatePath, []byte(commit+"\n"), 0o644)
}

func shortUpdateCommit(commit string) string {
	commit = strings.TrimSpace(commit)
	if len(commit) <= 12 {
		return commit
	}
	return commit[:12]
}
