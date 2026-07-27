package platformsupport

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
)

type Level string

const (
	Supported    Level = "supported"
	Experimental Level = "experimental"
	Unsupported  Level = "unsupported"
)

type Platform struct {
	GOOS      string   `json:"goos"`
	GOARCH    string   `json:"goarch"`
	Level     Level    `json:"level"`
	Transport string   `json:"transport"`
	Service   string   `json:"service"`
	OCI       string   `json:"oci"`
	Notes     []string `json:"notes"`
}

type Report struct {
	Current Platform   `json:"current"`
	Matrix  []Platform `json:"matrix"`
}

func Matrix() []Platform {
	m := []Platform{
		{GOOS: "linux", GOARCH: "amd64", Level: Supported, Transport: "Unix domain socket", Service: "systemd user", OCI: "Docker rootless or Podman rootless", Notes: []string{"primary release target", "SQLite uses system libsqlite3 through cgo"}},
		{GOOS: "linux", GOARCH: "arm64", Level: Experimental, Transport: "Unix domain socket", Service: "systemd user", OCI: "Docker rootless or Podman rootless", Notes: []string{"native CI runner required for cgo release certification"}},
		{GOOS: "darwin", GOARCH: "amd64", Level: Experimental, Transport: "Unix domain socket", Service: "launchd user", OCI: "Docker Desktop or Podman machine; enforced certification pending", Notes: []string{"build and unit-test target"}},
		{GOOS: "darwin", GOARCH: "arm64", Level: Experimental, Transport: "Unix domain socket", Service: "launchd user", OCI: "Docker Desktop or Podman machine; enforced certification pending", Notes: []string{"Apple Silicon build and unit-test target"}},
		{GOOS: "windows", GOARCH: "amd64", Level: Unsupported, Transport: "named-pipe implementation required", Service: "Windows service implementation required", OCI: "not designed", Notes: []string{"do not run through compatibility layers for enforced mode", "explicitly unsupported until named-pipe and credential isolation are implemented"}},
	}
	sort.Slice(m, func(i, j int) bool {
		if m[i].GOOS == m[j].GOOS {
			return m[i].GOARCH < m[j].GOARCH
		}
		return m[i].GOOS < m[j].GOOS
	})
	return m
}
func Current() Platform {
	for _, p := range Matrix() {
		if p.GOOS == runtime.GOOS && p.GOARCH == runtime.GOARCH {
			return p
		}
	}
	return Platform{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Level: Unsupported, Notes: []string{"platform is not in the tested support matrix"}}
}
func BuildReport() Report { return Report{Current: Current(), Matrix: Matrix()} }
func WriteJSON(path string, r Report) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if path == "" || path == "-" {
		_, err = os.Stdout.Write(b)
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
func Summary(p Platform) string {
	return fmt.Sprintf("%s/%s: %s; transport=%s; service=%s", p.GOOS, p.GOARCH, p.Level, p.Transport, p.Service)
}
