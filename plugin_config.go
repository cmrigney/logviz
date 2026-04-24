package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ConfigField describes a single configurable field for a plugin (from its manifest).
type ConfigField struct {
	Type        string `json:"type"`
	Default     string `json:"default"`
	Description string `json:"description"`
}

// pluginConfigFile is the on-disk shape of ~/.config/logviz/plugins.json.
type pluginConfigFile struct {
	Version int                          `json:"version"`
	Plugins map[string]pluginConfigEntry `json:"plugins"`
}

// pluginConfigEntry holds per-plugin persisted state.
type pluginConfigEntry struct {
	Enabled bool              `json:"enabled"`
	Config  map[string]string `json:"config"`
}

// configPathOverride, when non-empty, is returned by configPath() instead of
// the platform default. Set this in tests to avoid touching real user config.
var configPathOverride string

// configPath returns the path to the unified plugin config file.
func configPath() string {
	if configPathOverride != "" {
		return configPathOverride
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		// Fallback to ~/.config if UserConfigDir fails.
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "logviz", "plugins.json")
}

// loadPluginConfig reads the plugin config file. A missing file is not an
// error — it returns an empty struct (all plugins enabled, empty configs).
func loadPluginConfig() (*pluginConfigFile, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &pluginConfigFile{
				Version: 1,
				Plugins: make(map[string]pluginConfigEntry),
			}, nil
		}
		return nil, fmt.Errorf("read plugin config: %w", err)
	}
	var cfg pluginConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse plugin config: %w", err)
	}
	if cfg.Plugins == nil {
		cfg.Plugins = make(map[string]pluginConfigEntry)
	}
	if cfg.Version != 1 {
		fmt.Fprintf(os.Stderr, "[plugin-config] warning: unknown config version %d, treating as v1\n", cfg.Version)
	}
	return &cfg, nil
}

// savePluginConfig atomically writes cfg to disk using a tmp-file + rename.
// The directory is created with 0o700 if it doesn't exist.
func savePluginConfig(cfg *pluginConfigFile) error {
	p := configPath()
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir plugin config dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal plugin config: %w", err)
	}
	data = append(data, '\n')

	// Write to a temp file in the same directory, then rename for atomicity.
	tmp, err := os.CreateTemp(dir, "plugins-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create tmp config: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Best-effort cleanup of temp file on error.
		if _, statErr := os.Stat(tmpName); statErr == nil {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write tmp config: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod tmp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close tmp config: %w", err)
	}
	if err := os.Rename(tmpName, p); err != nil {
		return fmt.Errorf("rename tmp config: %w", err)
	}
	return nil
}

// entryFor returns the config entry for the named plugin, or a default entry
// with Enabled=true and an empty Config map if the entry is absent.
func (cfg *pluginConfigFile) entryFor(name string) pluginConfigEntry {
	if entry, ok := cfg.Plugins[name]; ok {
		if entry.Config == nil {
			entry.Config = make(map[string]string)
		}
		return entry
	}
	return pluginConfigEntry{
		Enabled: true,
		Config:  make(map[string]string),
	}
}
