package buildinfo

import (
	"runtime"
	"runtime/debug"
)

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
	Dirty   = "unknown"
)

type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	Dirty     string `json:"dirty"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
	Module    string `json:"module"`
}

func Current() Info {
	info := Info{Version: Version, Commit: Commit, Date: Date, Dirty: Dirty, GoVersion: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH}
	if bi, ok := debug.ReadBuildInfo(); ok {
		info.Module = bi.Main.Path
		if info.Version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			info.Version = bi.Main.Version
		}
	}
	return info
}
