package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/SHOnnay/futurediff/internal/ratelimit"
)

func main() {
	policyPath := flag.String("policy", "", "rate policy JSON; omitted to display safe defaults")
	selfTest := flag.Bool("self-test", false, "exercise burst, rate and concurrency rejection")
	flag.Parse()
	p := ratelimit.Default()
	var err error
	if *policyPath != "" {
		p, err = ratelimit.Load(*policyPath)
		must(err)
	}
	l, err := ratelimit.New(p)
	must(err)
	result := map[string]any{"policy": p, "valid": true}
	if *selfTest {
		now := time.Unix(100, 0)
		release, _, err := l.Begin("self-test", true, now)
		must(err)
		_, _, concurrencyErr := l.Begin("self-test", true, now)
		release()
		result["self_test"] = map[string]any{"concurrency_rejected": concurrencyErr != nil, "status": l.Status()}
		if concurrencyErr == nil {
			must(fmt.Errorf("concurrency self-test did not reject"))
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	must(enc.Encode(result))
}
func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
