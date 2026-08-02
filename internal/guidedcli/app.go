package guidedcli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultVerificationPolicy = `{
  "format_version": "0.1",
  "contract_id": "basic-repository-check",
  "policy_version": "policy-0.1",
  "checks": [
    {
      "check_id": "readme-exists",
      "required": true,
      "executor": "workspace_assertion",
      "type": "file_exists",
      "path": "README.md"
    }
  ]
}`

type App struct {
	In                 io.Reader
	Out                io.Writer
	Err                io.Writer
	Engine             Engine
	Daemon             DaemonManager
	Store              StateStore
	Renderer           Renderer
	Binary             string
	DaemonBinary       string
	Socket             string
	Paths              PathConfig
	InitErr            error
	JSON               bool
	Yes                bool
	Interactive        bool
	Verbose            bool
	VerifyPolicy       string
	CredentialConfig   string
	GitHubCredentialID string
	GitBinary          string
}

type Options struct {
	Binary             string
	DaemonBinary       string
	Home               string
	Socket             string
	StatePath          string
	VerifyPolicy       string
	CredentialConfig   string
	GitHubCredentialID string
	JSON               bool
	Yes                bool
	Verbose            bool
	NoColor            bool
	NonInteractive     bool
}

func New(options Options) *App {
	binary := findBinary(options.Binary, "FUTUREDIFF_BINARY", "futurediff")
	daemonBinary := findBinary(options.DaemonBinary, "FUTUREDIFF_DAEMON_BINARY", "futurediffd")
	paths, pathErr := resolvePathConfig(options)
	socket := paths.Socket.Path
	statePath := paths.State.Path
	if pathErr != nil {
		socket = strings.TrimSpace(options.Socket)
		statePath = strings.TrimSpace(options.StatePath)
	}
	credentialConfig := credentialConfigPath(options.CredentialConfig)
	githubCredentialID := strings.TrimSpace(options.GitHubCredentialID)
	if githubCredentialID == "" {
		githubCredentialID = strings.TrimSpace(os.Getenv("FUTUREDIFF_GITHUB_CREDENTIAL_ID"))
	}
	interactive := !options.NonInteractive && stdinInteractive(os.Stdin) && writerIsTerminal(os.Stdout)
	engine := ExecEngine{Binary: binary, Socket: socket}
	renderer := NewRenderer(os.Stdout, os.Stderr, options.NoColor || options.JSON)
	app := &App{
		In: os.Stdin, Out: os.Stdout, Err: os.Stderr,
		Engine: engine,
		Store:  StateStore{Path: statePath}, Renderer: renderer,
		Binary: binary, DaemonBinary: daemonBinary, Socket: socket,
		Paths: paths, InitErr: pathErr,
		JSON: options.JSON, Yes: options.Yes, Interactive: interactive, Verbose: options.Verbose,
		VerifyPolicy: options.VerifyPolicy, CredentialConfig: credentialConfig,
		GitHubCredentialID: githubCredentialID, GitBinary: "git",
	}
	app.Daemon = DaemonManager{
		Engine: engine, Binary: daemonBinary, Socket: socket,
		Root: paths.Home.Path, CredentialConfig: credentialConfig,
	}
	return app
}

func stdinInteractive(in *os.File) bool {
	info, err := in.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (a *App) Run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		if a.InitErr != nil {
			return a.fail(a.InitErr)
		}
		if err := a.landing(); err != nil {
			return a.fail(err)
		}
		return 0
	}
	command := args[0]
	args = args[1:]
	if a.InitErr != nil && command != "help" && command != "--help" && command != "-h" && command != "version" && command != "--version" && command != "completion" {
		return a.fail(a.InitErr)
	}
	var err error
	switch command {
	case "help", "--help", "-h":
		a.help()
	case "menu":
		if !a.Interactive {
			err = errors.New("fdif menu requires an interactive terminal")
		} else {
			err = a.menu(ctx)
		}
	case "version", "--version":
		err = a.version(ctx)
	case "start", "create", "new":
		err = a.start(ctx, args)
	case "status", "get":
		err = a.status(ctx, args)
	case "workspace":
		err = a.workspace(ctx, args)
	case "shell":
		err = a.shell(ctx, args)
	case "review":
		err = a.review(ctx, args)
	case "seal":
		err = a.seal(ctx, args)
	case "verify":
		err = a.verify(ctx, args)
	case "approve":
		err = a.approve(ctx, args)
	case "publish", "apply", "commit":
		err = a.apply(ctx, args)
	case "finish":
		err = a.finish(ctx, args)
	case "recover":
		err = a.recoverCommand(ctx, args)
	case "transactions", "list":
		err = a.transactions(ctx)
	case "use":
		err = a.use(ctx, args)
	case "events":
		err = a.passthroughTransaction(ctx, "events", args)
	case "abort", "discard":
		err = a.abort(ctx, args)
	case "daemon":
		err = a.daemon(ctx, args)
	case "doctor":
		err = a.doctor(ctx)
	case "config":
		err = a.config(args)
	case "demo":
		err = a.demo(ctx, args)
	case "completion":
		err = a.completion(args)
	default:
		err = fmt.Errorf("unknown command %q; run fdif --help", command)
	}
	if err != nil {
		return a.fail(err)
	}
	return 0
}

func (a *App) fail(err error) int {
	code := 1
	var commandErr *CommandError
	if errors.As(err, &commandErr) && commandErr.ExitCode > 0 {
		code = commandErr.ExitCode
	}
	if a.JSON {
		_ = writeJSON(a.Out, map[string]any{"ok": false, "error": err.Error(), "exit_code": code})
	} else {
		fmt.Fprintln(a.Err, "fdif:", err)
	}
	return code
}

func (a *App) help() {
	fmt.Fprintln(a.Out, `FutureDiff guided CLI

Review AI-assisted changes in a safe working copy before they reach GitHub or
your current branch.

First steps:
  fdif                         show a newcomer-oriented starting point
  fdif start|new [repo]        create and select a safe working copy
  fdif demo [--yes]            run a disposable end-to-end demonstration
  fdif doctor                  check local requirements and paths

Everyday workflow:
  fdif status [tx]             show the current change and next step
  fdif workspace [tx]          print the safe working-copy path
  fdif shell [tx]              open a shell in the safe working copy
  fdif review [tx] [--full]    review changed files and the exact diff
  fdif finish [tx] [--yes]     verify, approve and publish a safe local branch
  fdif finish [tx] --github    also push the safe branch and create a draft PR
  fdif abort|discard [tx]      discard an unfinished change

Advanced workflow:
  fdif menu                    open the interactive menu
  fdif seal [tx]               freeze the exact reviewed patch
  fdif verify [tx]             run the configured verification policy
  fdif approve [tx]            approve the exact verified version
  fdif publish [tx]            publish futurediff/<transaction-id> locally
  fdif transactions            list changes
  fdif use [tx]                select the current change
  fdif recover [tx] [--yes]      recover an interrupted publish (canonical)
  fdif use --clear               clear a stale current-change selection
  fdif events [tx]             show audit events
  fdif daemon <action>         status, start, stop, restart or logs
  fdif config [--explain]      show effective paths and their sources
  fdif completion <shell>      print Bash, Zsh, Fish or PowerShell completion
  fdif version                 show version information

Path model:
  --home PATH                  set one FutureDiff home for state, daemon,
                               runtime files and safe working copies
  FDIF_HOME                    environment equivalent of --home
  FUTUREDIFF_ROOT              supported as a legacy home setting
  --state PATH                 advanced override for only the current-selection
                               file; it does not relocate daemon workspaces

Notes:
  - start and new are equivalent.
  - abort and discard are equivalent.
  - cooperative mode is the public-alpha default, not an OS sandbox.
  - finish publishes a new local branch; it does not modify your current branch.
  - --github is optional and requires a daemon credential configuration.
  - GitHub options: --remote, --base, --title, --body, --body-file,
    --github-credential.

Global options are handled by cmd/fdif:
  --home, --root, --binary, --daemon-binary, --socket, --state, --policy,
  --credential-config, --github-credential,
  --json, --yes, --verbose, --no-color, --non-interactive`)
}

func (a *App) landing() error {
	current, loadErr := a.Store.Load()
	hasCurrent := loadErr == nil && current.TransactionID != ""
	if loadErr != nil && !errors.Is(loadErr, os.ErrNotExist) {
		return loadErr
	}
	if a.JSON {
		result := map[string]any{
			"kind":    "fdif-start",
			"product": "FutureDiff",
			"purpose": "Review AI-assisted changes before they reach GitHub or your current branch.",
			"commands": map[string]string{
				"start":  "fdif start",
				"doctor": "fdif doctor",
				"demo":   "fdif demo --yes",
				"help":   "fdif --help",
			},
			"active_change": hasCurrent,
		}
		if hasCurrent {
			result["continue"] = map[string]string{
				"status": "fdif status",
				"review": "fdif review --full",
				"finish": "fdif finish",
			}
			if a.Verbose {
				result["transaction_id"] = current.TransactionID
			}
		}
		return writeJSON(a.Out, result)
	}
	a.Renderer.title("FutureDiff")
	fmt.Fprintln(a.Out, "Review AI-assisted changes before they reach GitHub or your current branch.")
	fmt.Fprintln(a.Out)
	if hasCurrent {
		a.Renderer.success("Active change detected")
		if a.Verbose {
			fmt.Fprintln(a.Out, "  Change ID:", current.TransactionID)
		}
		fmt.Fprintln(a.Out, "  Inspect:  fdif status")
		fmt.Fprintln(a.Out, "  Review:   fdif review --full")
		fmt.Fprintln(a.Out, "  Finish:   fdif finish")
		fmt.Fprintln(a.Out)
	}
	fmt.Fprintln(a.Out, "Start a change:")
	fmt.Fprintln(a.Out, "  fdif start")
	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Check your setup:")
	fmt.Fprintln(a.Out, "  fdif doctor")
	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Try a disposable demo:")
	fmt.Fprintln(a.Out, "  fdif demo --yes")
	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Run fdif --help for all commands. Use fdif menu for the interactive menu.")
	return nil
}

func (a *App) menu(ctx context.Context) error {
	a.Renderer.title("FutureDiff")
	fmt.Fprintln(a.Out, "Review changes safely before they reach your branch")
	fmt.Fprintln(a.Out)
	if a.Daemon.Status(ctx) == nil {
		a.Renderer.success("Daemon running")
	} else {
		a.Renderer.warning("Daemon not running")
	}
	if repo, err := detectRepository(ctx, a.GitBinary, ""); err == nil {
		fmt.Fprintln(a.Out, "Repository:", repo)
	}
	if current, err := a.Store.Load(); err == nil {
		fmt.Fprintln(a.Out, "Current change:", current.TransactionID)
	}
	fmt.Fprintln(a.Out, `
1  Start a new change
2  View current change
3  Review changed files
4  Finish and publish a safe local branch
5  Finish and create a GitHub draft pull request
6  View all changes
7  Start or check daemon
8  Run system check
9  Help
0  Exit`)
	choice, err := a.prompt("Select [0-9]: ")
	if err != nil {
		return err
	}
	switch strings.TrimSpace(choice) {
	case "0", "":
		return nil
	case "1":
		return a.start(ctx, nil)
	case "2":
		return a.status(ctx, nil)
	case "3":
		return a.review(ctx, nil)
	case "4":
		return a.finish(ctx, nil)
	case "5":
		return a.finish(ctx, []string{"--github"})
	case "6":
		return a.transactions(ctx)
	case "7":
		return a.daemon(ctx, []string{"status"})
	case "8":
		return a.doctor(ctx)
	case "9":
		a.help()
		return nil
	default:
		return errors.New("invalid menu selection")
	}
}

func (a *App) ensureDaemon(ctx context.Context) error {
	if a.Daemon.Status(ctx) == nil {
		return nil
	}
	if !a.Interactive && !a.Yes {
		return errors.New("FutureDiff daemon is not running; run fdif daemon start")
	}
	if !a.Yes {
		ok, err := a.confirm("FutureDiff daemon is not running. Start it now?", "YES")
		if err != nil || !ok {
			return errors.New("daemon start declined")
		}
	}
	if err := a.Daemon.Start(ctx); err != nil {
		return err
	}
	if !a.JSON {
		a.Renderer.success("Daemon started")
	}
	return nil
}

func (a *App) start(ctx context.Context, args []string) error {
	if err := a.ensureDaemon(ctx); err != nil {
		return err
	}
	repository := ""
	mode := "cooperative"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--mode":
			if i+1 >= len(args) {
				return errors.New("--mode requires a value")
			}
			i++
			mode = args[i]
		default:
			if !strings.HasPrefix(args[i], "-") && repository == "" {
				repository = args[i]
			}
		}
	}
	var err error
	repository, err = detectRepository(ctx, a.GitBinary, repository)
	if err != nil && a.Interactive {
		repository, err = a.prompt("Repository path: ")
		if err == nil {
			repository, err = detectRepository(ctx, a.GitBinary, strings.TrimSpace(repository))
		}
	}
	if err != nil {
		return err
	}
	raw, err := a.Engine.Run(ctx, "create", repository, mode)
	if err != nil {
		return err
	}
	response, err := decodeResponse(raw)
	if err != nil {
		return err
	}
	if response.Transaction == nil || response.Transaction.TransactionID == "" {
		return errors.New("create response did not include a transaction")
	}
	repoRoot := repository
	if response.Workspace != nil && response.Workspace.RepositoryRoot != "" {
		repoRoot = response.Workspace.RepositoryRoot
	}
	if err := a.Store.Save(response.Transaction.TransactionID, repoRoot); err != nil {
		return err
	}
	if a.JSON {
		return writeRawJSON(a.Out, raw)
	}
	a.Renderer.success("Safe working copy created")
	workspace := ""
	if response.Workspace != nil {
		workspace = response.Workspace.WorkspacePath
	}
	a.Renderer.fields([2]string{"Open", workspace}, [2]string{"Repository", repoRoot})
	if a.Verbose {
		a.Renderer.fields(
			[2]string{"Change ID", response.Transaction.TransactionID},
			[2]string{"Mode", response.Transaction.Mode},
			[2]string{"FutureDiff home", a.Paths.Home.Path},
		)
	}
	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Your current branch was not modified.")
	fmt.Fprintln(a.Out, "Work only in the safe working copy shown above.")
	fmt.Fprintln(a.Out, "When the change is ready, run:")
	fmt.Fprintln(a.Out, "  fdif review --full")
	fmt.Fprintln(a.Out, "  fdif finish")
	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Open it in your editor or coding agent, or run: fdif shell")
	return nil
}

func (a *App) status(ctx context.Context, args []string) error {
	id, err := a.resolveTransaction(ctx, firstPositional(args), false, false)
	if err != nil {
		return err
	}
	raw, response, err := a.get(ctx, id)
	if err != nil {
		return err
	}
	if a.JSON {
		return writeRawJSON(a.Out, raw)
	}
	a.Renderer.title("Current change")
	tx := response.Transaction
	workspace, repo := "", ""
	if response.Workspace != nil {
		workspace, repo = response.Workspace.WorkspacePath, response.Workspace.RepositoryRoot
	}
	changed := ""
	patchSize := ""
	if response.Patch != nil {
		changed = strings.Join(response.Patch.ChangedPaths, ", ")
		patchSize = formatBytes(response.Patch.PatchSizeBytes)
	}
	fields := [][2]string{
		{"Stage", friendlyStage(tx.Status)},
		{"Repository", repo},
		{"Safe working copy", workspace},
	}
	if changed != "" {
		fields = append(fields, [2]string{"Changed", changed})
	}
	if patchSize != "" {
		fields = append(fields, [2]string{"Patch", patchSize})
	}
	if a.Verbose {
		fields = append(fields,
			[2]string{"Change ID", tx.TransactionID},
			[2]string{"Internal status", tx.Status},
			[2]string{"Mode", tx.Mode},
		)
	}
	a.Renderer.fields(fields...)
	next := nextAction(tx.Status)
	if hasGitHubEffects(response) {
		next = "fdif finish --github"
	} else if tx.Status == "ready" && tx.ApprovalDigest != "" {
		next = "fdif publish"
	}
	if next != "" {
		a.Renderer.next(next)
	}
	return nil
}

func (a *App) workspace(ctx context.Context, args []string) error {
	id, err := a.resolveTransaction(ctx, firstPositional(args), false, false)
	if err != nil {
		return err
	}
	_, response, err := a.get(ctx, id)
	if err != nil {
		return err
	}
	if response.Workspace == nil || response.Workspace.WorkspacePath == "" {
		return errors.New("change has no safe working-copy path")
	}
	if a.JSON {
		return writeJSON(a.Out, map[string]string{"transaction_id": id, "workspace_path": response.Workspace.WorkspacePath})
	}
	fmt.Fprintln(a.Out, response.Workspace.WorkspacePath)
	return nil
}

func (a *App) shell(ctx context.Context, args []string) error {
	id, err := a.resolveTransaction(ctx, firstPositional(args), false, false)
	if err != nil {
		return err
	}
	_, response, err := a.get(ctx, id)
	if err != nil {
		return err
	}
	if response.Workspace == nil || response.Workspace.WorkspacePath == "" {
		return errors.New("change has no safe working copy")
	}
	if !a.Interactive {
		return errors.New("fdif shell requires an interactive terminal")
	}
	shell, shellArgs := userShell()
	cmd := exec.CommandContext(ctx, shell, shellArgs...)
	cmd.Dir = response.Workspace.WorkspacePath
	cmd.Stdin, cmd.Stdout, cmd.Stderr = a.In, a.Out, a.Err
	return cmd.Run()
}

func userShell() (string, []string) {
	if runtime.GOOS == "windows" {
		if shell := os.Getenv("COMSPEC"); shell != "" {
			return shell, nil
		}
		return "powershell.exe", nil
	}
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell, nil
	}
	return "/bin/sh", nil
}

func (a *App) review(ctx context.Context, args []string) error {
	id, err := a.resolveTransaction(ctx, firstPositional(args), false, false)
	if err != nil {
		return err
	}
	_, response, err := a.get(ctx, id)
	if err != nil {
		return err
	}
	if response.Workspace == nil || response.Workspace.WorkspacePath == "" {
		return errors.New("change has no safe working copy")
	}
	full := contains(args, "--full")
	status, statusErr := gitOutput(ctx, a.GitBinary, response.Workspace.WorkspacePath, "status", "--short")
	stat, statErr := gitOutput(ctx, a.GitBinary, response.Workspace.WorkspacePath, "diff", "HEAD", "--stat", "--no-ext-diff")
	if statusErr != nil {
		return statusErr
	}
	if statErr != nil {
		return statErr
	}
	if a.JSON {
		result := map[string]any{"transaction_id": id, "workspace_path": response.Workspace.WorkspacePath, "status": strings.Split(strings.TrimSpace(status), "\n"), "diff_stat": strings.TrimSpace(stat)}
		if full {
			diff, _ := gitOutput(ctx, a.GitBinary, response.Workspace.WorkspacePath, "diff", "HEAD", "--no-ext-diff", "--binary")
			result["diff"] = diff
		}
		return writeJSON(a.Out, result)
	}
	title := "Review change"
	if a.Verbose {
		title += " " + shortID(id)
	}
	a.Renderer.title(title)
	if strings.TrimSpace(status) == "" {
		fmt.Fprintln(a.Out, "No changes detected in the safe working copy.")
	} else {
		fmt.Fprintln(a.Out, "\nChanged files")
		fmt.Fprintln(a.Out, indent(status, "  "))
	}
	if strings.TrimSpace(stat) != "" {
		fmt.Fprintln(a.Out, "\nSummary")
		fmt.Fprintln(a.Out, indent(stat, "  "))
	}
	if full {
		diff, err := gitOutput(ctx, a.GitBinary, response.Workspace.WorkspacePath, "diff", "HEAD", "--no-ext-diff", "--binary")
		if err != nil {
			return err
		}
		fmt.Fprintln(a.Out, "\nDiff")
		fmt.Fprintln(a.Out, diff)
	}
	if response.Transaction != nil && nextAction(response.Transaction.Status) != "" {
		a.Renderer.next(nextAction(response.Transaction.Status))
	}
	return nil
}

func (a *App) seal(ctx context.Context, args []string) error {
	id, err := a.resolveTransaction(ctx, firstPositional(args), false, true)
	if err != nil {
		return err
	}
	_, current, err := a.get(ctx, id)
	if err != nil {
		return err
	}
	if current.Transaction != nil && current.Transaction.Status != "active" {
		if a.JSON {
			return writeJSON(a.Out, current)
		}
		fmt.Fprintf(a.Out, "Transaction is already %s; seal was not repeated.\n", current.Transaction.Status)
		return nil
	}
	raw, err := a.Engine.Run(ctx, "seal", id)
	if err != nil {
		return err
	}
	response, err := decodeResponse(raw)
	if err != nil {
		return err
	}
	if a.JSON {
		return writeRawJSON(a.Out, raw)
	}
	a.Renderer.success("Reviewed version frozen")
	if response.Patch != nil {
		a.Renderer.fields([2]string{"Change ID", id}, [2]string{"Changed", strings.Join(response.Patch.ChangedPaths, ", ")}, [2]string{"Patch size", formatBytes(response.Patch.PatchSizeBytes)}, [2]string{"Patch SHA", shortHash(response.Patch.PatchSHA256)}, [2]string{"Status", response.Transaction.Status})
	}
	a.Renderer.next("fdif verify")
	return nil
}

func (a *App) verify(ctx context.Context, args []string) error {
	id, err := a.resolveTransaction(ctx, firstPositional(args), false, true)
	if err != nil {
		return err
	}
	policyPath := a.VerifyPolicy
	for i := 0; i < len(args); i++ {
		if args[i] == "--policy" && i+1 < len(args) {
			policyPath = args[i+1]
		}
	}
	cleanup := func() {}
	if policyPath == "" {
		file, createErr := os.CreateTemp("", "fdif-basic-*.verify.json")
		if createErr != nil {
			return createErr
		}
		if _, createErr = file.WriteString(defaultVerificationPolicy); createErr != nil {
			file.Close()
			os.Remove(file.Name())
			return createErr
		}
		if createErr = file.Close(); createErr != nil {
			os.Remove(file.Name())
			return createErr
		}
		policyPath = file.Name()
		cleanup = func() { _ = os.Remove(file.Name()) }
	}
	defer cleanup()
	raw, err := a.Engine.Run(ctx, "verify", id, policyPath)
	if err != nil {
		return err
	}
	response, err := decodeResponse(raw)
	if err != nil {
		return err
	}
	if a.JSON {
		return writeRawJSON(a.Out, raw)
	}
	a.Renderer.success("Checks passed")
	status := "ready"
	if response.Transaction != nil {
		status = response.Transaction.Status
	}
	a.Renderer.fields([2]string{"Change ID", id}, [2]string{"Status", status})
	a.Renderer.next("fdif approve")
	return nil
}

func (a *App) approve(ctx context.Context, args []string) error {
	id, err := a.resolveTransaction(ctx, firstPositional(args), false, true)
	if err != nil {
		return err
	}
	_, response, err := a.get(ctx, id)
	if err != nil {
		return err
	}
	if response.Transaction != nil && response.Transaction.Status == "ready" && response.Transaction.ApprovalDigest != "" {
		if !a.JSON {
			fmt.Fprintln(a.Out, "This exact reviewed version is already approved.")
		}
		return nil
	}
	material, err := a.approvalMaterial(ctx, id)
	if err != nil {
		return err
	}
	if !a.Yes {
		if !a.Interactive || a.JSON {
			return errors.New("approval requires an interactive terminal or explicit --yes")
		}
		if !a.JSON {
			a.approvalSummary(response, material.TransactionDigest)
		}
		ok, confirmErr := a.confirm("Approve this exact verified change?", "YES")
		if confirmErr != nil {
			return confirmErr
		}
		if !ok {
			return errors.New("approval declined")
		}
	}
	material, err = a.approvalMaterial(ctx, id)
	if err != nil {
		return err
	}
	raw, err := a.Engine.Run(ctx, "approve", id, material.TransactionDigest)
	if err != nil {
		return err
	}
	if a.JSON {
		return writeRawJSON(a.Out, raw)
	}
	a.Renderer.success("Exact reviewed version approved")
	a.Renderer.next("fdif publish")
	return nil
}

func (a *App) apply(ctx context.Context, args []string) error {
	id, err := a.resolveTransaction(ctx, firstPositional(args), false, true)
	if err != nil {
		return err
	}
	return a.publish(ctx, id, nil)
}

func (a *App) publish(ctx context.Context, id string, target *githubTarget) error {
	_, response, err := a.get(ctx, id)
	if err != nil {
		return err
	}
	if hasPreparedEffects(response) && target == nil {
		return errors.New("this change contains prepared external actions; use fdif finish --github for the guided GitHub flow or inspect it with the low-level CLI")
	}
	if response.Transaction != nil && (response.Transaction.Status == "committed" || response.Transaction.Status == "complete") {
		return a.renderPublishResult(ctx, id, response, target)
	}
	if response.Transaction == nil || response.Transaction.Status != "ready" {
		return fmt.Errorf("change must pass checks before publish; current status is %q", transactionStatus(response.Transaction))
	}
	if response.Transaction.ApprovalDigest == "" {
		return errors.New("change passed checks but is not approved; run fdif approve")
	}
	material, err := a.approvalMaterial(ctx, id)
	if err != nil {
		return err
	}
	if !a.Yes {
		if !a.Interactive || a.JSON {
			return errors.New("publish requires an interactive terminal or explicit --yes")
		}
		repo, changed := "", ""
		if response.Workspace != nil {
			repo = response.Workspace.RepositoryRoot
		}
		if response.Patch != nil {
			changed = strings.Join(response.Patch.ChangedPaths, ", ")
		}
		if target == nil {
			a.Renderer.title("Publish safe local branch")
			fields := [][2]string{
				{"Repository", repo},
				{"Reviewed files", changed},
				{"Exact version", shortHash(material.TransactionDigest)},
			}
			if a.Verbose {
				fields = append(fields, [2]string{"Change ID", id})
			}
			a.Renderer.fields(fields...)
			ok, confirmErr := a.confirm("Publish this exact approved change as a new local branch?", "PUBLISH")
			if confirmErr != nil {
				return confirmErr
			}
			if !ok {
				return errors.New("publish declined")
			}
		} else {
			a.Renderer.title("Publish and create GitHub draft pull request")
			a.Renderer.fields(
				[2]string{"Repository", repo},
				[2]string{"GitHub", target.Owner + "/" + target.Repo},
				[2]string{"Base", target.Base},
				[2]string{"Safe branch", target.Head},
				[2]string{"Pull request", target.Title},
				[2]string{"Reviewed files", changed},
				[2]string{"Exact version", shortHash(material.TransactionDigest)},
			)
			ok, confirmErr := a.confirm("Publish this exact approved change and create a draft pull request?", "SEND")
			if confirmErr != nil {
				return confirmErr
			}
			if !ok {
				return errors.New("GitHub publication declined")
			}
		}
	}
	material, err = a.approvalMaterial(ctx, id)
	if err != nil {
		return err
	}
	raw, err := a.Engine.Run(ctx, "commit", id, material.TransactionDigest)
	if err != nil {
		if response.Workspace != nil && localBranchExists(ctx, a.GitBinary, response.Workspace.RepositoryRoot, "futurediff/"+id) {
			return fmt.Errorf("local safe branch was materialized, but publication did not complete; run fdif status %s and futurediff recover %s: %w", id, id, err)
		}
		return err
	}
	final, err := decodeResponse(raw)
	if err != nil {
		return err
	}
	return a.renderPublishResult(ctx, id, final, target)
}

func (a *App) renderPublishResult(ctx context.Context, id string, response Response, target *githubTarget) error {
	repo := ""
	if response.Workspace != nil {
		repo = response.Workspace.RepositoryRoot
	}
	branch := "futurediff/" + id
	commitOID := ""
	if repo != "" {
		if resolved, resolveErr := gitOutput(ctx, a.GitBinary, repo, "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}"); resolveErr == nil {
			commitOID = strings.TrimSpace(resolved)
		}
	}
	status := "committed"
	if response.Transaction != nil && response.Transaction.Status != "" {
		status = response.Transaction.Status
	}
	var github *GitHubPublishResult
	if target != nil {
		result := githubResult(response, *target)
		github = &result
	}
	if a.JSON {
		result := map[string]any{
			"kind":                     "fdif-publish",
			"transaction_id":           id,
			"repository":               repo,
			"status":                   status,
			"published_branch":         branch,
			"commit_oid":               commitOID,
			"current_branch_unchanged": true,
			"canonical_response":       response,
		}
		if github != nil {
			result["github"] = github
		}
		_ = a.Store.Clear()
		return writeJSON(a.Out, result)
	}
	if github == nil {
		a.Renderer.success("Reviewed change published locally")
	} else {
		a.Renderer.success("Reviewed change published and sent to GitHub")
	}
	a.Renderer.fields([2]string{"Safe branch", branch}, [2]string{"Commit", shortHash(commitOID)}, [2]string{"Current branch", "unchanged"})
	if a.Verbose {
		a.Renderer.fields([2]string{"Change ID", id}, [2]string{"Status", status})
	}
	if github != nil {
		a.Renderer.fields([2]string{"GitHub", github.Owner + "/" + github.Repo}, [2]string{"Draft PR", github.PullRequestURL})
		if github.URLIsFallback {
			a.Renderer.warning("The exact pull-request receipt URL was unavailable; open the pull-request list shown above.")
		}
	} else if repo != "" {
		a.Renderer.next("review with: git -C " + strconv.Quote(repo) + " diff HEAD.." + branch)
	}
	_ = a.Store.Clear()
	return nil
}

func localBranchExists(ctx context.Context, gitBinary, repository, branch string) bool {
	if repository == "" {
		return false
	}
	_, err := gitOutput(ctx, gitBinary, repository, "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}")
	return err == nil
}

func (a *App) finish(ctx context.Context, args []string) error {
	options, err := parseFinishOptions(args, a.GitHubCredentialID)
	if err != nil {
		return err
	}
	id, err := a.resolveTransaction(ctx, firstPositional(args), false, true)
	if err != nil {
		return err
	}
	_, initial, err := a.get(ctx, id)
	if err != nil {
		return err
	}
	if !options.Enabled && hasPreparedEffects(initial) {
		return errors.New("this change contains prepared external actions; rerun the matching guided provider flow or inspect it with the low-level CLI. Local-only finish cannot silently commit external actions")
	}
	if !a.JSON {
		if err := a.review(ctx, append([]string{id}, optionalFlag(contains(args, "--full"), "--full")...)); err != nil {
			return err
		}
	}
	var target *githubTarget
	if options.Enabled {
		resolved, targetErr := resolveGitHubTarget(ctx, a.GitBinary, initial, id, options)
		if targetErr != nil {
			return targetErr
		}
		target = &resolved
		if hasPreparedEffects(initial) {
			if _, _, matchErr := matchingGitHubEffects(initial.Effects, resolved); matchErr != nil {
				return matchErr
			}
		}
		if !a.JSON {
			a.Renderer.title("GitHub draft pull request selected")
			a.Renderer.fields([2]string{"Repository", resolved.Owner + "/" + resolved.Repo}, [2]string{"Remote", resolved.Remote}, [2]string{"Base", resolved.Base}, [2]string{"Safe branch", resolved.Head}, [2]string{"Title", resolved.Title})
		}
	}
	for step := 0; step < 8; step++ {
		_, response, getErr := a.get(ctx, id)
		if getErr != nil {
			return getErr
		}
		if response.Transaction == nil {
			return errors.New("transaction response missing status")
		}
		switch response.Transaction.Status {
		case "active":
			if err := a.runStep(func() error { return a.seal(ctx, []string{id}) }); err != nil {
				return err
			}
		case "sealed":
			if target != nil {
				updated, prepareErr := a.prepareGitHubEffects(ctx, id, response, *target)
				if prepareErr != nil {
					return fmt.Errorf("prepare GitHub draft pull request: %w", prepareErr)
				}
				response = updated
			}
			verifyArgs := []string{id}
			if a.VerifyPolicy != "" {
				verifyArgs = append(verifyArgs, "--policy", a.VerifyPolicy)
			}
			if err := a.runStep(func() error { return a.verify(ctx, verifyArgs) }); err != nil {
				return err
			}
		case "ready":
			if target != nil {
				branch, draft, matchErr := matchingGitHubEffects(response.Effects, *target)
				if matchErr != nil {
					return matchErr
				}
				if branch == nil || draft == nil {
					return errors.New("GitHub publication was selected after verification; effects must be prepared while the change is sealed. Start a new change or finish this one locally")
				}
			}
			if response.Transaction.ApprovalDigest == "" {
				if err := a.runStep(func() error { return a.approve(ctx, append([]string{id}, optionalFlag(a.Yes, "--yes")...)) }); err != nil {
					return err
				}
				continue
			}
			return a.publish(ctx, id, target)
		case "committed", "complete":
			return a.renderPublishResult(ctx, id, response, target)
		case "aborted":
			return errors.New("cannot finish an aborted transaction")
		default:
			return fmt.Errorf("unsupported transaction status %q", response.Transaction.Status)
		}
	}
	return errors.New("finish exceeded the expected state transition count")
}

func (a *App) transactions(ctx context.Context) error {
	raw, err := a.Engine.Run(ctx, "list")
	if err != nil {
		return err
	}
	response, err := decodeResponse(raw)
	if err != nil {
		return err
	}
	if a.JSON {
		return writeRawJSON(a.Out, raw)
	}
	currentID := ""
	if current, loadErr := a.Store.Load(); loadErr == nil {
		currentID = current.TransactionID
	}
	sort.Slice(response.Transactions, func(i, j int) bool { return response.Transactions[i].CreatedAt > response.Transactions[j].CreatedAt })
	a.Renderer.title("Changes")
	fmt.Fprintf(a.Out, "%-8s %-18s %-12s %s\n", "CURRENT", "CHANGE ID", "STATUS", "SAFE WORKING COPY")
	for _, tx := range response.Transactions {
		marker := ""
		if tx.TransactionID == currentID {
			marker = "*"
		}
		fmt.Fprintf(a.Out, "%-8s %-18s %-12s %s\n", marker, shortID(tx.TransactionID), tx.Status, tx.WorkspaceIdentity)
	}
	if len(response.Transactions) == 0 {
		fmt.Fprintln(a.Out, "No changes.")
	}
	return nil
}

func (a *App) use(ctx context.Context, args []string) error {
	if contains(args, "--clear") {
		if err := a.Store.Clear(); err != nil {
			return err
		}
		if a.JSON {
			return writeJSON(a.Out, map[string]string{"kind": "fdif-use-clear", "current_transaction_id": ""})
		}
		a.Renderer.success("Current change selection cleared")
		return nil
	}
	id := firstPositional(args)
	if id == "" {
		var err error
		id, err = a.resolveTransaction(ctx, "", true, false)
		if err != nil {
			return err
		}
	}
	_, response, err := a.get(ctx, id)
	if err != nil {
		return err
	}
	repo := ""
	if response.Workspace != nil {
		repo = response.Workspace.RepositoryRoot
	}
	if err := a.Store.Save(id, repo); err != nil {
		return err
	}
	if a.JSON {
		return writeJSON(a.Out, map[string]string{"current_transaction_id": id, "repository_root": repo})
	}
	a.Renderer.success("Current change selected: " + id)
	return nil
}

func (a *App) abort(ctx context.Context, args []string) error {
	id, err := a.resolveTransaction(ctx, firstPositional(args), false, true)
	if err != nil {
		return err
	}
	if !a.Yes {
		if !a.Interactive || a.JSON {
			return errors.New("discard requires an interactive terminal or explicit --yes")
		}
		ok, confirmErr := a.confirm("Discard change "+id+"?", "DISCARD")
		if confirmErr != nil {
			return confirmErr
		}
		if !ok {
			return errors.New("discard declined")
		}
	}
	raw, err := a.Engine.Run(ctx, "abort", id)
	if err != nil {
		return err
	}
	if current, loadErr := a.Store.Load(); loadErr == nil && current.TransactionID == id {
		_ = a.Store.Clear()
	}
	if a.JSON {
		return writeRawJSON(a.Out, raw)
	}
	a.Renderer.success("Change discarded")
	return nil
}

func (a *App) passthroughTransaction(ctx context.Context, command string, args []string) error {
	id, err := a.resolveTransaction(ctx, firstPositional(args), false, false)
	if err != nil {
		return err
	}
	raw, err := a.Engine.Run(ctx, command, id)
	if err != nil {
		return err
	}
	return writeRawJSON(a.Out, raw)
}

func (a *App) daemon(ctx context.Context, args []string) error {
	action := "status"
	if len(args) > 0 {
		action = args[0]
	}
	switch action {
	case "status":
		err := a.Daemon.Status(ctx)
		if a.JSON {
			return writeJSON(a.Out, map[string]any{"running": err == nil, "socket": a.Socket})
		}
		if err == nil {
			a.Renderer.success("FutureDiff daemon is running")
			return nil
		}
		a.Renderer.warning("FutureDiff daemon is not running")
		return nil
	case "start":
		a.Daemon.UnsafeNoAuth = contains(args, "--unsafe-disable-peer-auth")
		if a.Daemon.UnsafeNoAuth && !a.Yes {
			if !a.Interactive {
				return errors.New("unsafe peer-auth disable requires --yes")
			}
			ok, err := a.confirm("Disable daemon peer authentication for development?", "UNSAFE")
			if err != nil || !ok {
				return errors.New("unsafe daemon start declined")
			}
		}
		if err := a.Daemon.Start(ctx); err != nil {
			return err
		}
		if !a.JSON {
			a.Renderer.success("FutureDiff daemon started")
		}
		return nil
	case "stop":
		if err := a.Daemon.Stop(); err != nil {
			return err
		}
		if !a.JSON {
			a.Renderer.success("FutureDiff daemon stop requested")
		}
		return nil
	case "restart":
		_ = a.Daemon.Stop()
		time.Sleep(300 * time.Millisecond)
		if err := a.Daemon.Start(ctx); err != nil {
			return err
		}
		if !a.JSON {
			a.Renderer.success("FutureDiff daemon restarted")
		}
		return nil
	case "logs":
		root := a.Paths.Home.Path
		if root == "" {
			root = a.Daemon.Root
		}
		logPath := filepath.Join(root, "futurediffd.log")
		if a.JSON {
			return writeJSON(a.Out, map[string]string{"log_path": logPath})
		}
		fmt.Fprintln(a.Out, logPath)
		return nil
	default:
		return fmt.Errorf("unknown daemon action %q", action)
	}
}

func (a *App) doctor(ctx context.Context) error {
	type check struct{ ID, Status, Detail string }
	checks := []check{}
	if path, err := resolveExecutable(a.Binary); err == nil {
		checks = append(checks, check{"futurediff_binary", "pass", path})
	} else {
		checks = append(checks, check{"futurediff_binary", "fail", err.Error()})
	}
	if path, err := resolveExecutable(a.DaemonBinary); err == nil {
		checks = append(checks, check{"futurediffd_binary", "pass", path})
	} else {
		checks = append(checks, check{"futurediffd_binary", "fail", err.Error()})
	}
	if path, err := exec.LookPath(a.GitBinary); err == nil {
		checks = append(checks, check{"git", "pass", path})
	} else {
		checks = append(checks, check{"git", "fail", err.Error()})
	}
	if a.Paths.Home.Path != "" {
		if _, err := os.Lstat(a.Paths.Home.Path); os.IsNotExist(err) {
			checks = append(checks, check{"futurediff_home", "pass", a.Paths.Home.Path + " (will be created with private permissions)"})
		} else if err != nil {
			checks = append(checks, check{"futurediff_home", "fail", err.Error()})
		} else if err := validatePrivateDirectory(a.Paths.Home.Path); err != nil {
			checks = append(checks, check{"futurediff_home", "fail", err.Error()})
		} else {
			checks = append(checks, check{"futurediff_home", "pass", a.Paths.Home.Path})
		}
		checks = append(checks, check{"safe_workspaces", "pass", a.Paths.WorkspaceRoot.Path})
	}
	if err := a.Daemon.Status(ctx); err == nil {
		checks = append(checks, check{"daemon", "pass", "running"})
	} else {
		checks = append(checks, check{"daemon", "warn", "not running"})
	}
	if a.CredentialConfig == "" {
		checks = append(checks, check{"github_credentials", "warn", "not configured; local publication remains available"})
	} else if info, err := os.Lstat(a.CredentialConfig); err != nil {
		checks = append(checks, check{"github_credentials", "fail", err.Error()})
	} else if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		checks = append(checks, check{"github_credentials", "fail", "credential config must be a regular file"})
	} else if info.Mode().Perm()&0o077 != 0 {
		checks = append(checks, check{"github_credentials", "fail", "credential config permissions must be 0600"})
	} else if a.GitHubCredentialID == "" {
		checks = append(checks, check{"github_credentials", "warn", "config file is present but no GitHub credential ID is selected"})
	} else {
		checks = append(checks, check{"github_credentials", "pass", "configured as " + a.GitHubCredentialID})
	}
	if a.JSON {
		return writeJSON(a.Out, map[string]any{"kind": "fdif-doctor", "checks": checks})
	}
	a.Renderer.title("FutureDiff guided CLI doctor")
	for _, item := range checks {
		switch item.Status {
		case "pass":
			a.Renderer.success(item.ID + ": " + item.Detail)
		case "warn":
			a.Renderer.warning(item.ID + ": " + item.Detail)
		default:
			fmt.Fprintln(a.Out, "x", item.ID+":", item.Detail)
		}
	}
	return nil
}

func resolveExecutable(binary string) (string, error) {
	if filepath.IsAbs(binary) || strings.ContainsRune(binary, os.PathSeparator) {
		info, err := os.Stat(binary)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			return "", fmt.Errorf("%s is a directory", binary)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
			return "", fmt.Errorf("%s is not executable", binary)
		}
		return binary, nil
	}
	return exec.LookPath(binary)
}

func pathOr(explicit, found string) string {
	if found != "" {
		return found
	}
	return explicit
}

func (a *App) config(args []string) error {
	explain := false
	for _, arg := range args {
		switch arg {
		case "--explain":
			explain = true
		default:
			return fmt.Errorf("unknown config option %q", arg)
		}
	}
	config := Config{
		Binary: a.Binary, DaemonBinary: a.DaemonBinary,
		Home: a.Paths.Home.Path, Socket: a.Socket, StatePath: a.Store.Path,
		RuntimePath: a.Paths.Runtime.Path, WorkspaceRoot: a.Paths.WorkspaceRoot.Path,
		Paths:            a.Paths,
		VerifyPolicy:     pathOr(a.VerifyPolicy, "embedded basic policy"),
		CredentialConfig: a.CredentialConfig, GitHubCredentialID: a.GitHubCredentialID,
		JSON: a.JSON, Interactive: a.Interactive, Verbose: a.Verbose,
		Color: a.Renderer.Color, Unicode: a.Renderer.Unicode,
	}
	if a.JSON {
		return writeJSON(a.Out, config)
	}
	if explain {
		a.Renderer.title("FutureDiff paths")
		printResolvedPath(a.Out, "Home", a.Paths.Home)
		printResolvedPath(a.Out, "Current selection", a.Paths.State)
		printResolvedPath(a.Out, "Daemon socket", a.Paths.Socket)
		printResolvedPath(a.Out, "Runtime", a.Paths.Runtime)
		printResolvedPath(a.Out, "Safe workspaces", a.Paths.WorkspaceRoot)
		fmt.Fprintln(a.Out)
		fmt.Fprintln(a.Out, "The advanced --state option changes only the current-selection file.")
		fmt.Fprintln(a.Out, "Use --home or FDIF_HOME to relocate daemon data and safe workspaces together.")
		return nil
	}
	a.Renderer.title("FutureDiff guided CLI configuration")
	a.Renderer.fields(
		[2]string{"home", config.Home},
		[2]string{"current selection", config.StatePath},
		[2]string{"runtime", config.RuntimePath},
		[2]string{"safe workspaces", config.WorkspaceRoot},
		[2]string{"socket", config.Socket},
		[2]string{"binary", config.Binary},
		[2]string{"daemon", config.DaemonBinary},
		[2]string{"verification", config.VerifyPolicy},
		[2]string{"credential config", cleanDisplayPath(config.CredentialConfig)},
		[2]string{"GitHub credential", pathOr(config.GitHubCredentialID, "not configured")},
		[2]string{"interactive", strconv.FormatBool(config.Interactive)},
		[2]string{"verbose", strconv.FormatBool(config.Verbose)},
		[2]string{"color", strconv.FormatBool(config.Color)},
		[2]string{"unicode", strconv.FormatBool(config.Unicode)},
	)
	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Run fdif config --explain to see where every path came from.")
	return nil
}

func printResolvedPath(out io.Writer, label string, value ResolvedPath) {
	fmt.Fprintln(out, label+":")
	fmt.Fprintln(out, "  "+cleanDisplayPath(value.Path))
	fmt.Fprintln(out, "  source: "+value.Source)
	fmt.Fprintln(out)
}

func (a *App) demo(ctx context.Context, args []string) error {
	if err := a.ensureDaemon(ctx); err != nil {
		return err
	}
	root, err := os.MkdirTemp("", "futurediff-fdif-demo-*")
	if err != nil {
		return err
	}
	keep := contains(args, "--keep")
	if !keep {
		defer os.RemoveAll(root)
	}
	commands := [][]string{{"init", "-b", "main"}, {"config", "user.name", "FutureDiff Demo"}, {"config", "user.email", "demo@localhost"}}
	for _, command := range commands {
		if _, err := gitOutput(ctx, a.GitBinary, root, command...); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello\n"), 0o644); err != nil {
		return err
	}
	if _, err := gitOutput(ctx, a.GitBinary, root, "add", "README.md"); err != nil {
		return err
	}
	if _, err := gitOutput(ctx, a.GitBinary, root, "commit", "-m", "demo: initial state"); err != nil {
		return err
	}
	if !a.JSON {
		a.Renderer.title("FutureDiff guided demo")
		fmt.Fprintln(a.Out, "Temporary repository:", root)
	}
	if err := a.runStep(func() error { return a.start(ctx, []string{root}) }); err != nil {
		return err
	}
	current, err := a.Store.Load()
	if err != nil {
		return err
	}
	_, response, err := a.get(ctx, current.TransactionID)
	if err != nil {
		return err
	}
	if response.Workspace == nil {
		return errors.New("demo transaction has no workspace")
	}
	file, err := os.OpenFile(filepath.Join(response.Workspace.WorkspacePath, "README.md"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.WriteString("future\n"); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	original, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		return err
	}
	if strings.Contains(string(original), "future") {
		return errors.New("isolation failure: source repository changed before publish")
	}
	beforeStatus, err := gitOutput(ctx, a.GitBinary, root, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(beforeStatus) != "" {
		return errors.New("isolation failure: source repository became dirty before publish")
	}
	if !a.JSON {
		a.Renderer.success("Isolation verified: source repository is unchanged")
	}
	oldYes := a.Yes
	if contains(args, "--yes") {
		a.Yes = true
	}
	defer func() { a.Yes = oldYes }()
	if err := a.runStep(func() error { return a.finish(ctx, []string{current.TransactionID}) }); err != nil {
		return err
	}
	currentBranchContent, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		return err
	}
	if strings.Contains(string(currentBranchContent), "future") {
		return errors.New("isolation failure: current branch changed during publish")
	}
	afterStatus, err := gitOutput(ctx, a.GitBinary, root, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(afterStatus) != "" {
		return errors.New("isolation failure: current source worktree became dirty during publish")
	}
	publishedBranch := "futurediff/" + current.TransactionID
	publishedContent, err := gitOutput(ctx, a.GitBinary, root, "show", publishedBranch+":README.md")
	if err != nil {
		return fmt.Errorf("verify published demo branch: %w", err)
	}
	if !strings.Contains(publishedContent, "future") {
		return errors.New("demo publish branch does not contain the staged change")
	}
	if a.JSON {
		return writeJSON(a.Out, map[string]any{
			"ok":                          true,
			"repository":                  root,
			"transaction_id":              current.TransactionID,
			"published_branch":            publishedBranch,
			"source_before_publish_clean": true,
			"current_branch_unchanged":    true,
			"published_branch_verified":   true,
			"kept":                        keep,
		})
	}
	a.Renderer.success("Demo completed")
	a.Renderer.fields(
		[2]string{"Repository", root},
		[2]string{"Current branch", "unchanged"},
		[2]string{"Published branch", publishedBranch},
		[2]string{"Published content", strings.TrimSpace(publishedContent)},
		[2]string{"Cleanup", map[bool]string{true: "kept", false: "automatic"}[keep]},
	)
	return nil
}

func (a *App) completion(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: fdif completion <bash|zsh|fish|powershell>")
	}
	script, err := completionScript(args[0])
	if err != nil {
		return err
	}
	_, err = io.WriteString(a.Out, script)
	return err
}

func (a *App) version(ctx context.Context) error {
	raw, err := a.Engine.Run(ctx, "version")
	if err != nil {
		return err
	}
	if a.JSON {
		return writeRawJSON(a.Out, raw)
	}
	var value map[string]any
	if json.Unmarshal(raw, &value) == nil {
		a.Renderer.title("FutureDiff")
		for _, key := range []string{"version", "commit", "date", "dirty"} {
			if item, ok := value[key]; ok {
				fmt.Fprintf(a.Out, "  %-8s  %v\n", key, item)
			}
		}
		fmt.Fprintln(a.Out, "  guided   fdif")
		return nil
	}
	_, err = a.Out.Write(raw)
	return err
}

func (a *App) runStep(fn func() error) error {
	if !a.JSON {
		return fn()
	}
	oldOut := a.Out
	oldRendererOut := a.Renderer.Out
	a.Out = io.Discard
	a.Renderer.Out = io.Discard
	defer func() { a.Out = oldOut; a.Renderer.Out = oldRendererOut }()
	return fn()
}

func (a *App) resolveTransaction(ctx context.Context, explicit string, forceSelection bool, strict bool) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	hadSelection := false
	if !forceSelection {
		if current, err := a.Store.Load(); err == nil {
			hadSelection = true
			if _, _, getErr := a.get(ctx, current.TransactionID); getErr == nil {
				return current.TransactionID, nil
			} else if strict {
				return "", fmt.Errorf("selected change %s is not available; run fdif use or fdif recover: %w", current.TransactionID, getErr)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("current-change selection is unreadable; run fdif use --clear: %w", err)
		}
	}
	raw, err := a.Engine.Run(ctx, "list")
	if err != nil {
		return "", err
	}
	response, err := decodeResponse(raw)
	if err != nil {
		return "", err
	}
	eligible := make([]Transaction, 0)
	for _, tx := range response.Transactions {
		if tx.Status != "aborted" && tx.Status != "committed" && tx.Status != "complete" {
			eligible = append(eligible, tx)
		}
	}
	if strict && len(eligible) > 0 && len(eligible) < len(response.Transactions) {
		return "", errors.New("multiple changes exist and the selected change is unavailable; run fdif use <transaction-id> to choose explicitly")
	}
	if len(eligible) == 1 {
		if strict && !a.Interactive {
			return "", errors.New("no current change is selected; run fdif use " + eligible[0].TransactionID)
		}
		if !hadSelection {
			_ = a.Store.Save(eligible[0].TransactionID, "")
		}
		return eligible[0].TransactionID, nil
	}
	if len(eligible) == 0 {
		return "", errors.New("no active FutureDiff change; run fdif start")
	}
	if !a.Interactive {
		return "", errors.New("multiple changes exist; specify an ID or run fdif use")
	}
	a.Renderer.title("Select change")
	for i, tx := range eligible {
		fmt.Fprintf(a.Out, "%d  %-18s %s\n", i+1, shortID(tx.TransactionID), tx.Status)
	}
	value, err := a.prompt("Select: ")
	if err != nil {
		return "", err
	}
	index, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || index < 1 || index > len(eligible) {
		return "", errors.New("invalid transaction selection")
	}
	selected := eligible[index-1]
	_ = a.Store.Save(selected.TransactionID, "")
	return selected.TransactionID, nil
}

func (a *App) get(ctx context.Context, id string) ([]byte, Response, error) {
	raw, err := a.Engine.Run(ctx, "get", id)
	if err != nil {
		return nil, Response{}, err
	}
	response, err := decodeResponse(raw)
	return raw, response, err
}

func (a *App) approvalMaterial(ctx context.Context, id string) (ApprovalMaterial, error) {
	raw, err := a.Engine.Run(ctx, "approval-material", id)
	if err != nil {
		return ApprovalMaterial{}, err
	}
	return decodeApprovalMaterial(raw)
}

func (a *App) approvalSummary(response Response, digest string) {
	a.Renderer.title("Approve exact reviewed version")
	repo, changed, patch := "", "", ""
	if response.Workspace != nil {
		repo = response.Workspace.RepositoryRoot
	}
	if response.Patch != nil {
		changed = strings.Join(response.Patch.ChangedPaths, ", ")
		patch = shortHash(response.Patch.PatchSHA256)
	}
	a.Renderer.fields([2]string{"Repository", repo}, [2]string{"Changed", changed}, [2]string{"Patch SHA", patch}, [2]string{"Exact version", shortHash(digest)})
}

func (a *App) prompt(label string) (string, error) {
	fmt.Fprint(a.Out, label)
	reader := bufio.NewReader(a.In)
	value, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func (a *App) confirm(question, expected string) (bool, error) {
	value, err := a.prompt(fmt.Sprintf("%s Type %s: ", question, expected))
	if err != nil {
		return false, err
	}
	return value == expected, nil
}

func detectRepository(ctx context.Context, gitBinary, path string) (string, error) {
	if path == "" {
		path = "."
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	bare, bareErr := gitOutput(ctx, gitBinary, absolute, "rev-parse", "--is-bare-repository")
	if bareErr == nil && strings.TrimSpace(bare) == "true" {
		return "", fmt.Errorf("%s is a bare Git repository; use a checked-out worktree on a branch", absolute)
	}
	output, err := gitOutput(ctx, gitBinary, absolute, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%s is not a Git repository", absolute)
	}
	root := strings.TrimSpace(output)
	headRef, err := gitOutput(ctx, gitBinary, root, "symbolic-ref", "-q", "HEAD")
	if err != nil || strings.TrimSpace(headRef) == "" {
		return "", fmt.Errorf("%s has a detached HEAD; checkout a branch before running fdif start", root)
	}
	return root, nil
}

func gitCommandEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_EXTERNAL_DIFF=",
		"GIT_DIFF_OPTS=",
		"GIT_PAGER=cat",
		"PAGER=cat",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"SSH_ASKPASS=",
	}
}

func gitOutput(ctx context.Context, binary, directory string, args ...string) (string, error) {
	cmdArgs := []string{
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "credential.helper=",
		"-c", "diff.external=",
		"-c", "core.pager=cat",
		"-C", directory,
	}
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.CommandContext(ctx, binary, cmdArgs...)
	cmd.Env = gitCommandEnv()
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func firstPositional(args []string) string {
	valueOptions := map[string]bool{
		"--policy": true, "--mode": true, "--remote": true, "--base": true,
		"--title": true, "--body": true, "--body-file": true,
		"--github-credential": true,
	}
	for i := 0; i < len(args); i++ {
		if valueOptions[args[i]] {
			i++
			continue
		}
		if !strings.HasPrefix(args[i], "-") {
			return args[i]
		}
	}
	return ""
}

func contains(args []string, target string) bool {
	for _, arg := range args {
		if arg == target {
			return true
		}
	}
	return false
}
func optionalFlag(enabled bool, flag string) []string {
	if enabled {
		return []string{flag}
	}
	return nil
}
func shortID(value string) string {
	if len(value) > 18 {
		return value[:15] + "..."
	}
	return value
}
func shortHash(value string) string {
	if len(value) > 15 {
		return value[:12] + "..."
	}
	return value
}
func formatBytes(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%d bytes", value)
	}
	return fmt.Sprintf("%.1f KiB", float64(value)/1024)
}
func indent(value, prefix string) string {
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}
func friendlyStage(status string) string {
	switch status {
	case "active":
		return "Editing"
	case "sealed":
		return "Change frozen"
	case "ready":
		return "Checks passed; approval required"
	case "approved":
		return "Approved; ready to publish"
	case "committed", "complete":
		return "Published"
	case "aborted":
		return "Discarded"
	case "failed":
		return "Needs attention"
	case "":
		return "Unknown"
	default:
		return status
	}
}

func nextAction(status string) string {
	switch status {
	case "active":
		return "fdif review, then fdif finish"
	case "sealed":
		return "fdif verify"
	case "ready":
		return "fdif finish"
	default:
		return ""
	}
}

func transactionStatus(tx *Transaction) string {
	if tx == nil {
		return "missing"
	}
	return tx.Status
}

func writeRawJSON(out io.Writer, raw []byte) error {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	return writeJSON(out, value)
}
