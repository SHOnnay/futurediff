package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/supportbundle"
	"os"
	"path/filepath"
	"time"
)

func main() {
	root := flag.String("root", defaultRoot(), "FutureDiff data root")
	output := flag.String("output", "", "output zip")
	verify := flag.String("verify", "", "verify an existing support bundle")
	socket := flag.String("socket", "", "daemon socket")
	runtime := flag.String("runtime", "", "optional docker or podman runtime")
	credential := flag.String("credential-config", "", "credential metadata file path")
	timeout := flag.Duration("timeout", 15*time.Second, "diagnostic timeout")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	if *verify != "" {
		m, e := supportbundle.Verify(*verify)
		if e != nil {
			fail(e)
		}
		b, _ := json.MarshalIndent(m, "", "  ")
		fmt.Println(string(b))
		return
	}
	if *output == "" {
		fail(fmt.Errorf("--output is required"))
	}
	if *socket == "" {
		*socket = filepath.Join(*root, "futurediff.sock")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	m, e := supportbundle.Create(ctx, *output, supportbundle.Options{DataRoot: *root, Socket: *socket, Runtime: *runtime, CredentialConfig: *credential})
	if e != nil {
		fail(e)
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	fmt.Println(string(b))
}
func defaultRoot() string { h, _ := os.UserHomeDir(); return filepath.Join(h, ".futurediff") }
func fail(e error)        { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
