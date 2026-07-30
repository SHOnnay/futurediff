package main

import "testing"

func TestParseArgumentsAllowsGlobalFlagsAfterCommand(t *testing.T) {
	options, args, err := parseArguments([]string{"finish", "tx_1", "--yes", "--json", "--binary", "/tmp/futurediff"})
	if err != nil {
		t.Fatal(err)
	}
	if !options.Yes || !options.JSON || options.Binary != "/tmp/futurediff" {
		t.Fatalf("unexpected options: %+v", options)
	}
	if len(args) != 2 || args[0] != "finish" || args[1] != "tx_1" {
		t.Fatalf("unexpected remaining args: %v", args)
	}
}

func TestParseArgumentsAcceptsGitHubCredentialOptions(t *testing.T) {
	options, args, err := parseArguments([]string{
		"finish", "tx_1", "--github",
		"--credential-config", "/tmp/providers.json",
		"--github-credential=github-main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.CredentialConfig != "/tmp/providers.json" || options.GitHubCredentialID != "github-main" {
		t.Fatalf("unexpected GitHub options: %+v", options)
	}
	if len(args) != 3 || args[0] != "finish" || args[1] != "tx_1" || args[2] != "--github" {
		t.Fatalf("unexpected remaining args: %v", args)
	}
}

func TestParseArgumentsPreservesFinishOptionValuesThatLookGlobal(t *testing.T) {
	options, args, err := parseArguments([]string{"finish", "--github", "--body", "--yes", "--title", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if options.Yes || options.JSON {
		t.Fatalf("finish option values were misread as global flags: %+v", options)
	}
	want := []string{"finish", "--github", "--body", "--yes", "--title", "--json"}
	if len(args) != len(want) {
		t.Fatalf("remaining args = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("remaining args = %v, want %v", args, want)
		}
	}
}
