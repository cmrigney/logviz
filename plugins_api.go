package main

import (
	"fmt"
)

// PluginInfo is the frontend-facing representation of a plugin.
type PluginInfo struct {
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
			Name:         spec.name,
			Enabled:      spec.enabled,
			Running:      pm.isRunning(spec.name),
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
func (a *App) SetPluginConfig(name string, cfg map[string]string) error {
	pm := a.plugins
	if pm == nil {
		return fmt.Errorf("plugin manager not initialised")
	}

	// Update allSpecs.
	pm.mu.Lock()
	found := false
	for i := range pm.allSpecs {
		if pm.allSpecs[i].name == name {
			if cfg == nil {
				cfg = make(map[string]string)
			}
			pm.allSpecs[i].config = cfg
			found = true
			break
		}
	}
	pm.mu.Unlock()
	if !found {
		return fmt.Errorf("plugin %q not found", name)
	}

	// Persist.
	if err := a.savePluginsConfig(); err != nil {
		return err
	}

	// Restart.
	return pm.restartPlugin(name)
}

// SetPluginEnabled enables or disables a plugin, persists the change, and
// starts or stops the plugin accordingly.
func (a *App) SetPluginEnabled(name string, enabled bool) error {
	pm := a.plugins
	if pm == nil {
		return fmt.Errorf("plugin manager not initialised")
	}

	pm.mu.Lock()
	found := false
	for i := range pm.allSpecs {
		if pm.allSpecs[i].name == name {
			pm.allSpecs[i].enabled = enabled
			found = true
			break
		}
	}
	pm.mu.Unlock()
	if !found {
		return fmt.Errorf("plugin %q not found", name)
	}

	// Persist.
	if err := a.savePluginsConfig(); err != nil {
		return err
	}

	// Restart (will stop if disabled, relaunch if enabled).
	return pm.restartPlugin(name)
}

// RestartPlugin manually restarts a named plugin (for debugging).
func (a *App) RestartPlugin(name string) error {
	pm := a.plugins
	if pm == nil {
		return fmt.Errorf("plugin manager not initialised")
	}
	return pm.restartPlugin(name)
}

// savePluginsConfig serialises the current allSpecs state into the config file.
func (a *App) savePluginsConfig() error {
	pm := a.plugins
	pm.mu.Lock()
	specs := make([]pluginSpec, len(pm.allSpecs))
	copy(specs, pm.allSpecs)
	pm.mu.Unlock()

	cfg := &pluginConfigFile{
		Version: 1,
		Plugins: make(map[string]pluginConfigEntry, len(specs)),
	}
	for _, spec := range specs {
		c := spec.config
		if c == nil {
			c = make(map[string]string)
		}
		cfg.Plugins[spec.name] = pluginConfigEntry{
			Enabled: spec.enabled,
			Config:  c,
		}
	}
	return savePluginConfig(cfg)
}
