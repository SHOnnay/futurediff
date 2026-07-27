package credentials

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
)

func LoadConfig(path string) (Config, error) {
	if path == "" {
		return Config{}, errors.New("credential config path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return Config{}, err
	}
	if !info.Mode().IsRegular() {
		return Config{}, errors.New("credential config must be a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return Config{}, fmt.Errorf("credential config permissions must not grant group or other access; found %04o", info.Mode().Perm())
	}
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	var config Config
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, err
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config.Canonicalize(), nil
}
