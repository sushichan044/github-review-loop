package xdg

import (
	"os"
	"path/filepath"
	"runtime"
)

func ConfigHome() (string, error) {
	cfgHome, err := getConfigHome()
	if err != nil {
		return "", err
	}

	return filepath.Clean(cfgHome), nil
}

func getConfigHome() (string, error) {
	if runtime.GOOS == "windows" {
		return os.UserConfigDir()
	}

	if configHome := os.Getenv("XDG_CONFIG_HOME"); configHome != "" {
		return configHome, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}
