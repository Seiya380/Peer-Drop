package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

type Config struct {
	DeviceName  string `json:"device_name"`
	Port        int    `json:"port"`
	DownloadDir string `json:"download_dir"`
}

func DefaultConfig() *Config {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "Peer-Drop Device"
	}

	downloadDir := getDefaultDownloadDir()

	return &Config{
		DeviceName:  hostname,
		Port:        8080,
		DownloadDir: downloadDir,
	}
}

func getDefaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, "Downloads", "PeerDrop")
}

func getConfigPath() string {
	var configDir string

	switch runtime.GOOS {
	case "windows":
		configDir = os.Getenv("APPDATA")
		if configDir == "" {
			configDir = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
	default:
		configDir = os.Getenv("XDG_CONFIG_HOME")
		if configDir == "" {
			home, _ := os.UserHomeDir()
			configDir = filepath.Join(home, ".config")
		}
	}

	return filepath.Join(configDir, "peerdrop", "config.json")
}

func Load() (*Config, error) {
	cfg := DefaultConfig()

	configPath := getConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Save() error {
	configPath := getConfigPath()

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

func (c *Config) EnsureDownloadDir() error {
	return os.MkdirAll(c.DownloadDir, 0755)
}
