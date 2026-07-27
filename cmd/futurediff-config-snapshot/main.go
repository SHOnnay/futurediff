package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/configsnapshot"
)

type filesFlag []string

func (f *filesFlag) String() string     { return strings.Join(*f, ",") }
func (f *filesFlag) Set(v string) error { *f = append(*f, v); return nil }

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "build":
		build(os.Args[2:])
	case "verify":
		verify(os.Args[2:])
	case "version":
		printJSON(buildinfo.Current())
	default:
		usage()
	}
}

func build(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	var files filesFlag
	fs.Var(&files, "file", "configuration entry name=path or name=?path for optional (repeatable)")
	out := fs.String("output", "", "snapshot output")
	_ = fs.Parse(args)
	if *out == "" || len(files) == 0 {
		usage()
	}
	inputs := make([]configsnapshot.Input, 0, len(files))
	for _, raw := range files {
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 {
			fail(fmt.Errorf("invalid --file %q", raw))
		}
		path, required := parts[1], true
		if strings.HasPrefix(path, "?") {
			required = false
			path = strings.TrimPrefix(path, "?")
		}
		inputs = append(inputs, configsnapshot.Input{Name: parts[0], Path: path, Required: required})
	}
	m, err := configsnapshot.Build(inputs, time.Now())
	must(err)
	must(configsnapshot.Write(*out, m))
	printJSON(m)
}
func verify(args []string) {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	manifest := fs.String("manifest", "", "snapshot manifest")
	_ = fs.Parse(args)
	if *manifest == "" {
		usage()
	}
	m, err := configsnapshot.Load(*manifest)
	must(err)
	r, err := configsnapshot.Verify(m, time.Now())
	must(err)
	printJSON(r)
	if !r.Verified {
		os.Exit(1)
	}
}
func usage() {
	fmt.Fprintln(os.Stderr, "usage: futurediff-config-snapshot <build|verify|version> [flags]")
	os.Exit(2)
}
func must(err error) {
	if err != nil {
		fail(err)
	}
}
func fail(err error)  { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
func printJSON(v any) { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
