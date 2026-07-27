package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/integrations"
)

func main() {
	home, _ := os.UserHomeDir()
	target := flag.String("target", "", "integration target: opencode or hermes")
	mcp := flag.String("mcp-binary", "", "absolute path to futurediff-mcp")
	socket := flag.String("socket", filepath.Join(home, ".futurediff", "futurediff.sock"), "daemon Unix socket")
	output := flag.String("output", "-", "output file or - for stdout")
	strict := flag.Bool("strict", true, "generate a fail-closed agent profile")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	if *mcp == "" {
		if exe, err := os.Executable(); err == nil {
			*mcp = filepath.Join(filepath.Dir(exe), "futurediff-mcp")
		}
	}
	options := integrations.Options{MCPBinary: *mcp, Socket: *socket, Strict: *strict}
	var data []byte
	var err error
	switch *target {
	case "opencode":
		data, err = integrations.OpenCodeConfig(options)
	case "hermes":
		data, err = integrations.HermesConfig(options)
	default:
		err = fmt.Errorf("--target must be opencode or hermes")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if *output == "-" {
		fmt.Println(string(data))
		return
	}
	if err := integrations.WriteAtomic(*output, data); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Println(*output)
}
