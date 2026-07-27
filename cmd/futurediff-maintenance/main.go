package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/maintenance"
)

func main() {
	home, _ := os.UserHomeDir()
	root := flag.String("root", filepath.Join(home, ".futurediff"), "FutureDiff data root")
	action := flag.String("action", "status", "status, enable, or disable")
	reason := flag.String("reason", "", "maintenance reason")
	actor := flag.String("actor", "local-operator", "operator identity")
	ttl := flag.Duration("ttl", 0, "optional automatic expiry")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		printJSON(buildinfo.Current())
		return
	}
	m := &maintenance.Manager{Path: filepath.Join(*root, "maintenance.json")}
	var v any
	var err error
	switch *action {
	case "status":
		v, err = m.Status(time.Now())
	case "enable":
		v, err = m.Enable(*reason, *actor, *ttl, time.Now())
	case "disable":
		v, err = m.Disable(*actor, time.Now())
	default:
		err = fmt.Errorf("unsupported action %q", *action)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	printJSON(v)
}
func printJSON(v any) { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
