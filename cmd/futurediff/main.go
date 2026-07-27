package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SHOnnay/futurediff/internal/buildinfo"

	"github.com/SHOnnay/futurediff/internal/adapters/githubdraft"
	"github.com/SHOnnay/futurediff/internal/adapters/slackoutbox"
	"github.com/SHOnnay/futurediff/internal/api"
	"github.com/SHOnnay/futurediff/internal/app"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: futurediff [--socket path] <version|health|create|execute|get|prepare-github-branch|prepare-github-pr|prepare-slack-message|effects|refresh-effect|seal|verify|approval-material|approve|approve-signed|approve-quorum|commit|recover|abort|events> ...")
	os.Exit(2)
}
func main() {
	home, _ := os.UserHomeDir()
	socket := flag.String("socket", filepath.Join(home, ".futurediff", "futurediff.sock"), "daemon Unix socket")
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		usage()
	}
	client := api.NewClient(*socket)
	var method, path string
	var body any
	switch args[0] {
	case "version":
		encoded, _ := json.MarshalIndent(buildinfo.Current(), "", "  ")
		fmt.Println(string(encoded))
		return
	case "health":
		method, path = "GET", "/v1/health"
	case "create":
		if len(args) < 2 {
			usage()
		}
		mode := "cooperative"
		if len(args) >= 3 {
			mode = args[2]
		}
		method, path = "POST", "/v1/transactions"
		body = app.CreateRequest{Repository: args[1], Mode: mode, PolicyVersion: "policy-0.1"}
	case "execute":
		if len(args) < 3 {
			usage()
		}
		method, path = "POST", "/v1/transactions/"+args[1]+"/execute"
		body = app.ExecuteRequest{Command: args[2:]}
	case "prepare-github-branch":
		if len(args) < 7 {
			usage()
		}
		method, path = "POST", "/v1/transactions/"+args[1]+"/effects/github/branch"
		body = app.PrepareGitHubBranchRequest{CredentialID: args[2], Owner: args[3], Repo: args[4], Branch: args[5], RemoteURL: args[6]}
	case "prepare-github-pr":
		if len(args) < 8 {
			usage()
		}
		bodyText := ""
		if len(args) >= 9 {
			bodyText = args[8]
		}
		method, path = "POST", "/v1/transactions/"+args[1]+"/effects/github/draft-pull-request"
		input := githubdraft.Input{Owner: args[3], Repo: args[4], Head: args[5], Base: args[6], Title: args[7], Body: bodyText}
		if len(args) >= 10 {
			input.DependsOnEffectID = args[9]
		}
		body = app.PrepareGitHubDraftPRRequest{CredentialID: args[2], Input: input}
	case "prepare-slack-message":
		if len(args) < 5 {
			usage()
		}
		input := slackoutbox.Input{Channel: args[3], Text: args[4]}
		if len(args) > 5 {
			input.DependsOn = args[5:]
		}
		method, path = "POST", "/v1/transactions/"+args[1]+"/effects/slack/message"
		body = app.PrepareSlackMessageRequest{CredentialID: args[2], Input: input}
	case "effects":
		if len(args) < 2 {
			usage()
		}
		method, path = "GET", "/v1/transactions/"+args[1]+"/effects"
	case "refresh-effect":
		if len(args) < 3 {
			usage()
		}
		method, path = "POST", "/v1/transactions/"+args[1]+"/effects/"+args[2]+"/refresh"
	case "get", "seal", "approval-material", "recover", "abort", "events":
		if len(args) < 2 {
			usage()
		}
		id := args[1]
		suffix := map[string]string{"get": "", "seal": "/seal", "approval-material": "/approval-material", "recover": "/recover", "abort": "/abort", "events": "/events"}[args[0]]
		method = "GET"
		if args[0] == "seal" || args[0] == "recover" || args[0] == "abort" {
			method = "POST"
		}
		path = "/v1/transactions/" + id + suffix
	case "verify":
		if len(args) < 3 {
			usage()
		}
		b, err := os.ReadFile(args[2])
		if err != nil {
			fatal(err)
		}
		var raw any
		if err := json.Unmarshal(b, &raw); err != nil {
			fatal(err)
		}
		method, path, body = "POST", "/v1/transactions/"+args[1]+"/verify", raw
	case "approve":
		if len(args) < 3 {
			usage()
		}
		method, path, body = "POST", "/v1/transactions/"+args[1]+"/approve", map[string]string{"transaction_digest": args[2], "approver": "local-user"}
	case "approve-signed":
		if len(args) < 3 {
			usage()
		}
		b, err := os.ReadFile(args[2])
		if err != nil {
			fatal(err)
		}
		var env any
		if err := json.Unmarshal(b, &env); err != nil {
			fatal(err)
		}
		method, path, body = "POST", "/v1/transactions/"+args[1]+"/approve", map[string]any{"approval_envelope": env}
	case "approve-quorum":
		if len(args) < 3 {
			usage()
		}
		b, err := os.ReadFile(args[2])
		if err != nil {
			fatal(err)
		}
		var bundle any
		if err := json.Unmarshal(b, &bundle); err != nil {
			fatal(err)
		}
		method, path, body = "POST", "/v1/transactions/"+args[1]+"/approve", map[string]any{"approval_bundle": bundle}
	case "commit":
		if len(args) < 3 {
			usage()
		}
		method, path, body = "POST", "/v1/transactions/"+args[1]+"/commit", map[string]string{"transaction_digest": args[2]}
	default:
		usage()
	}
	out, err := client.Do(method, path, body)
	if err != nil {
		fatal(err)
	}
	var pretty any
	if json.Unmarshal(out, &pretty) == nil {
		b, _ := json.MarshalIndent(pretty, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Println(string(out))
	}
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
