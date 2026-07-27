package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/futurediff/futurediff/control-plane/gateway"
)

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: futurediff <run|inspect|commit|recover> [args]")
	}

	service := gateway.SpikeService{}
	ctx := context.Background()

	switch os.Args[1] {
	case "run":
		runFlags := flag.NewFlagSet("run", flag.ExitOnError)
		repo := runFlags.String("repo", ".", "path to git repository")
		verifyShell := runFlags.String("verify-shell", "", "shell command to verify the staged worktree before approval")
		_ = runFlags.Parse(os.Args[2:])
		command := runFlags.Args()
		if len(command) == 0 {
			fatalf("run requires a command after flags")
		}
		options := gateway.RunOptions{Command: command}
		if strings.TrimSpace(*verifyShell) != "" {
			options.VerifyCommand = []string{"/bin/sh", "-c", *verifyShell}
		}
		record, err := service.RunWithOptions(ctx, *repo, options)
		if err != nil {
			fatalf("run failed: %v", err)
		}
		fmt.Printf("transaction=%s\nstate=%s\nworktree=%s\npatch=%s\n", record.ID, record.State, record.WorktreePath, record.PatchPath)
		if record.VerificationOutputPath != "" {
			fmt.Printf("verification_log=%s\n", record.VerificationOutputPath)
		}
	case "inspect":
		inspectFlags := flag.NewFlagSet("inspect", flag.ExitOnError)
		repo := inspectFlags.String("repo", ".", "path to git repository")
		tx := inspectFlags.String("tx", "", "transaction id")
		_ = inspectFlags.Parse(os.Args[2:])
		if strings.TrimSpace(*tx) == "" {
			fatalf("inspect requires --tx")
		}
		record, patch, err := service.Inspect(*repo, *tx)
		if err != nil {
			fatalf("inspect failed: %v", err)
		}
		fmt.Printf("transaction=%s\nstate=%s\nbase_rev=%s\n", record.ID, record.State, record.BaseRev)
		fmt.Print(patch)
	case "commit":
		commitFlags := flag.NewFlagSet("commit", flag.ExitOnError)
		repo := commitFlags.String("repo", ".", "path to git repository")
		tx := commitFlags.String("tx", "", "transaction id")
		_ = commitFlags.Parse(os.Args[2:])
		if strings.TrimSpace(*tx) == "" {
			fatalf("commit requires --tx")
		}
		record, err := service.Commit(ctx, *repo, *tx)
		if err != nil {
			fatalf("commit failed: %v", err)
		}
		fmt.Printf("transaction=%s\nstate=%s\n", record.ID, record.State)
	case "recover":
		recoverFlags := flag.NewFlagSet("recover", flag.ExitOnError)
		repo := recoverFlags.String("repo", ".", "path to git repository")
		tx := recoverFlags.String("tx", "", "transaction id")
		_ = recoverFlags.Parse(os.Args[2:])
		if strings.TrimSpace(*tx) == "" {
			fatalf("recover requires --tx")
		}
		record, err := service.Recover(*repo, *tx)
		if err != nil {
			fatalf("recover failed: %v", err)
		}
		fmt.Printf("transaction=%s\nstate=%s\n", record.ID, record.State)
	default:
		fatalf("unknown command: %s", os.Args[1])
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
