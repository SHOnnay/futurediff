package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/SHOnnay/futurediff/internal/guidedcli"
)

func main() {
	options, args, err := parseArguments(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "fdif:", err)
		os.Exit(2)
	}
	app := guidedcli.New(options)
	os.Exit(app.Run(context.Background(), args))
}

func parseArguments(args []string) (guidedcli.Options, []string, error) {
	var options guidedcli.Options
	remaining := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if isFinishValueOption(arg) {
			if i+1 >= len(args) {
				return options, nil, fmt.Errorf("%s requires a value", arg)
			}
			remaining = append(remaining, arg, args[i+1])
			i++
			continue
		}
		switch arg {
		case "--json":
			options.JSON = true
		case "--yes", "-y":
			options.Yes = true
		case "--no-color":
			options.NoColor = true
		case "--non-interactive":
			options.NonInteractive = true
		case "--binary", "--daemon-binary", "--socket", "--state", "--policy", "--credential-config", "--github-credential":
			if i+1 >= len(args) {
				return options, nil, fmt.Errorf("%s requires a value", arg)
			}
			i++
			setOption(&options, arg, args[i])
		default:
			if key, value, ok := strings.Cut(arg, "="); ok && isValueOption(key) {
				if value == "" {
					return options, nil, fmt.Errorf("%s requires a value", key)
				}
				setOption(&options, key, value)
				continue
			}
			remaining = append(remaining, arg)
		}
	}
	return options, remaining, nil
}

func isFinishValueOption(arg string) bool {
	switch arg {
	case "--remote", "--base", "--title", "--body", "--body-file":
		return true
	default:
		return false
	}
}

func isValueOption(key string) bool {
	switch key {
	case "--binary", "--daemon-binary", "--socket", "--state", "--policy", "--credential-config", "--github-credential":
		return true
	default:
		return false
	}
}

func setOption(options *guidedcli.Options, key, value string) {
	switch key {
	case "--binary":
		options.Binary = value
	case "--daemon-binary":
		options.DaemonBinary = value
	case "--socket":
		options.Socket = value
	case "--state":
		options.StatePath = value
	case "--policy":
		options.VerifyPolicy = value
	case "--credential-config":
		options.CredentialConfig = value
	case "--github-credential":
		options.GitHubCredentialID = value
	}
}
