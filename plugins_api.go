package main

import (
	"fmt"
)

// PluginInfo is the frontend-facing representation of a plugin.
//
// ID is the stable unique key (full absolute path of the plugin file).
// Name is the display-only basename.
// The frontend must pass ID — not Name — to SetPluginEnabled, SetPluginConfig,
// and RestartPlugin to avoid collisions when two plugins share a filename.
type PluginInfo struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Enabled      bool                   `json:"enabled"`
	Running      bool                   `json:"running"`
	Config       map[string]string      `json:"config"`
	ConfigSchema map[string]ConfigField `json:"configSchema,omitempty"`
	Description  string                 `json:"description,omitempty"`
	Protocol     int                    `json:"protocol"`
}

// ListPlugins returns info about all discovered plugins (including disabled ones).
func (a *App) ListPlugins() []PluginInfo {
	pm := a.plugins
	if pm == nil {
		return []PluginInfo{}
	}
	pm.mu.Lock()
	specs := make([]pluginSpec, len(pm.allSpecs))
	copy(specs, pm.allSpecs)
	pm.mu.Unlock()

	result := make([]PluginInfo, 0, len(specs))
	for _, spec := range specs {
		cfg := spec.config
		if cfg == nil {
			cfg = make(map[string]string)
		}
		info := PluginInfo{
			ID:           spec.id,
			Name:         spec.name,
			Enabled:      spec.enabled,
			Running:      pm.isRunning(spec.id),
			Config:       cfg,
			ConfigSchema: spec.configSchema,
			Description:  spec.description,
			Protocol:     spec.protocol,
		}
		result = append(result, info)
	}
	return result
}

// SetPluginConfig updates a plugin's config map, saves it to disk, and restarts
// the plugin (if enabled) so it receives the new hello message.
//
// id is the full absolute path of the plugin file (PluginInfo.ID).
//
// Save-first ordering: the config file is written before in-memory state is
// mutated. If the save fails the in-memory spec is left unchanged and the error
// is returned without restarting the plugin.
func (a *App) SetPluginConfig(id string, cfg map[string]string) error {
	pm := a.plugins
	if pm == nil {
		return fmt.Errorf("plugin manager not initialised")
	}
	if cfg == nil {
		cfg = make(map[string]string)
	}

	// Locate the target and build the config to save.
	pm.mu.Lock()
	idx := -1
	for i := range pm.allSpecs {
		if pm.allSpecs[i].id == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		pm.mu.Unlock()
		return fmt.Errorf("plugin %q not found", id)
	}
	fileCfg := a.buildConfigFile(pm, id, func(spec pluginSpec) pluginConfigEntry {
		return pluginConfigEntry{Enabled: spec.enabled, Config: cfg}
	})
	pm.mu.Unlock()

	// Persist first — do not touch in-memory state yet.
	if err := savePluginConfig(fileCfg); err != nil {
		return err
	}

	// Save succeeded: apply the change to in-memory state.
	pm.mu.Lock()
	pm.allSpecs[idx].config = cfg
	pm.mu.Unlock()

	// Restart so the plugin receives an updated hello.
	return pm.restartPlugin(id)
}

// SetPluginEnabled enables or disables a plugin, persists the change, and
// starts or stops the plugin accordingly.
//
// id is the full absolute path of the plugin file (PluginInfo.ID).
//
// Save-first ordering: the config file is written before in-memory state is
// mutated. If the save fails the in-memory spec is left unchanged and the error
// is returned without restarting the plugin.
func (a *App) SetPluginEnabled(id string, enabled bool) error {
	pm := a.plugins
	if pm == nil {
		return fmt.Errorf("plugin manager not initialised")
	}

	// Locate the target and build the config to save.
	pm.mu.Lock()
	idx := -1
	for i := range pm.allSpecs {
		if pm.allSpecs[i].id == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		pm.mu.Unlock()
		return fmt.Errorf("plugin %q not found", id)
	}
	fileCfg := a.buildConfigFile(pm, id, func(spec pluginSpec) pluginConfigEntry {
		return pluginConfigEntry{Enabled: enabled, Config: spec.config}
	})
	pm.mu.Unlock()

	// Persist first — do not touch in-memory state yet.
	if err := savePluginConfig(fileCfg); err != nil {
		return err
	}

	// Save succeeded: apply the change to in-memory state.
	pm.mu.Lock()
	pm.allSpecs[idx].enabled = enabled
	pm.mu.Unlock()

	// Restart (will stop if disabled, relaunch if enabled).
	return pm.restartPlugin(id)
}

// RestartPlugin manually restarts a plugin by its ID (full absolute path).
func (a *App) RestartPlugin(id string) error {
	pm := a.plugins
	if pm == nil {
		return fmt.Errorf("plugin manager not initialised")
	}
	return pm.restartPlugin(id)
}

// buildConfigFile constructs a pluginConfigFile from the current allSpecs,
// substituting the entry for overrideID using the provided override function.
// Must be called with pm.mu held.
// Keys in the config file are the plugin IDs (full absolute paths).
func (a *App) buildConfigFile(pm *pluginManager, overrideID string, override func(pluginSpec) pluginConfigEntry) *pluginConfigFile {
	fileCfg := &pluginConfigFile{
		Version: 1,
		Plugins: make(map[string]pluginConfigEntry, len(pm.allSpecs)),
	}
	for _, spec := range pm.allSpecs {
		var entry pluginConfigEntry
		if spec.id == overrideID {
			entry = override(spec)
		} else {
			c := spec.config
			if c == nil {
				c = make(map[string]string)
			}
			entry = pluginConfigEntry{Enabled: spec.enabled, Config: c}
		}
		if entry.Config == nil {
			entry.Config = make(map[string]string)
		}
		fileCfg.Plugins[spec.id] = entry
	}
	return fileCfg
}

// savePluginsConfig serialises the current allSpecs state into the config file.
func (a *App) savePluginsConfig() error {
	pm := a.plugins
	pm.mu.Lock()
	fileCfg := a.buildConfigFile(pm, "", func(spec pluginSpec) pluginConfigEntry {
		return pluginConfigEntry{} // never called; no spec has id == ""
	})
	pm.mu.Unlock()
	return savePluginConfig(fileCfg)
}
