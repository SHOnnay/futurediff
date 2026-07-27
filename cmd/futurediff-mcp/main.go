package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/mcpbridge"
)

func main() {
	home, _ := os.UserHomeDir()
	socket := flag.String("socket", filepath.Join(home, ".futurediff", "futurediff.sock"), "FutureDiff daemon Unix socket")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	server := mcpbridge.New(*socket)
	if err := server.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
