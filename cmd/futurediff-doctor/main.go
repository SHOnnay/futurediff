package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/doctor"
	"os"
	"path/filepath"
	"time"
)

func main() {
	home, _ := os.UserHomeDir()
	root := flag.String("root", filepath.Join(home, ".futurediff"), "FutureDiff data root")
	socket := flag.String("socket", "", "daemon Unix socket; defaults to <root>/futurediff.sock")
	creds := flag.String("credential-config", "", "credential metadata configuration")
	runtime := flag.String("runtime", "", "optional docker or podman rootless probe")
	timeout := flag.Duration("timeout", 10*time.Second, "diagnostic timeout")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	if *socket == "" {
		*socket = filepath.Join(*root, "futurediff.sock")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report := doctor.Run(ctx, doctor.Options{DataRoot: *root, Socket: *socket, CredentialConfig: *creds, Runtime: *runtime})
	b, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(b))
	if !report.Healthy {
		os.Exit(2)
	}
}
