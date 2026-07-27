package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/certification"
	"github.com/SHOnnay/futurediff/internal/runtimeoci"
)

func main() {
	kindFlag := flag.String("runtime", "docker", "OCI runtime: docker or podman")
	binary := flag.String("runtime-binary", "", "optional runtime binary path")
	image := flag.String("image", "", "digest-pinned certification image")
	output := flag.String("output", "-", "JSON report path or - for stdout")
	scratch := flag.String("scratch", "", "certification scratch directory")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	if *image == "" {
		fmt.Fprintln(os.Stderr, "error: --image with a sha256 digest is required")
		os.Exit(2)
	}
	kind := runtimeoci.Docker
	if *kindFlag == "podman" {
		kind = runtimeoci.Podman
	} else if *kindFlag != "docker" {
		fmt.Fprintln(os.Stderr, "error: --runtime must be docker or podman")
		os.Exit(2)
	}
	if *scratch == "" {
		home, _ := os.UserHomeDir()
		*scratch = filepath.Join(home, ".futurediff", "certification")
	}
	runner := &runtimeoci.Runner{Kind: kind, Binary: *binary, Policy: runtimeoci.DefaultPolicy(*image), ScratchRoot: filepath.Join(*scratch, "oci")}
	report, err := certification.Run(context.Background(), runner, *image, certification.Options{Root: *scratch})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if err := certification.WriteJSON(*output, report); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if !report.Certified {
		os.Exit(1)
	}
}
