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
