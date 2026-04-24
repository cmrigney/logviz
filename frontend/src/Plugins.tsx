import { useEffect, useState, useCallback } from 'react'
import { ListPlugins, SetPluginEnabled, SetPluginConfig, RestartPlugin } from '../wailsjs/go/main/App'
import { main } from '../wailsjs/go/models'

type PluginInfo = main.PluginInfo
type ConfigField = main.ConfigField

interface PluginsModalProps {
  onClose: () => void
}

// Local editable state for one plugin's config, keyed by plugin ID.
interface PluginEdit {
  config: Record<string, string>
  dirty: boolean
}

export function PluginsModal({ onClose }: PluginsModalProps) {
  const [plugins, setPlugins] = useState<PluginInfo[]>([])
  // edits is keyed by plugin.id (full path), not plugin.name.
  const [edits, setEdits] = useState<Record<string, PluginEdit>>({})
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const fetchPlugins = useCallback(async () => {
    try {
      const list = await ListPlugins()
      setPlugins(list ?? [])
      // Initialize edits for plugins that don't have local edits yet.
      // Key by id to avoid collisions between same-named plugins in different dirs.
      setEdits(prev => {
        const next = { ...prev }
        for (const p of (list ?? [])) {
          if (!next[p.id]) {
            next[p.id] = { config: { ...(p.config ?? {}) }, dirty: false }
          }
        }
        return next
      })
      setError(null)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchPlugins()
  }, [fetchPlugins])

  // All API calls use plugin.id (full path), not plugin.name.
  const handleToggle = async (id: string, enabled: boolean) => {
    try {
      setError(null)
      await SetPluginEnabled(id, enabled)
      await fetchPlugins()
    } catch (e) {
      setError(String(e))
    }
  }

  const handleSave = async (id: string) => {
    const edit = edits[id]
    if (!edit) return
    try {
      setError(null)
      await SetPluginConfig(id, edit.config)
      await fetchPlugins()
      setEdits(prev => ({
        ...prev,
        [id]: { ...prev[id], dirty: false },
      }))
    } catch (e) {
      setError(String(e))
    }
  }

  const handleRevert = (id: string, plugin: PluginInfo) => {
    setEdits(prev => ({
      ...prev,
      [id]: { config: { ...(plugin.config ?? {}) }, dirty: false },
    }))
  }

  const handleRestart = async (id: string) => {
    try {
      setError(null)
      await RestartPlugin(id)
      await fetchPlugins()
    } catch (e) {
      setError(String(e))
    }
  }

  const setConfigField = (id: string, key: string, value: string) => {
    setEdits(prev => ({
      ...prev,
      [id]: {
        config: { ...(prev[id]?.config ?? {}), [key]: value },
        dirty: true,
      },
    }))
  }

  const addConfigKey = (id: string) => {
    const key = prompt('New config key:')
    if (!key) return
    setEdits(prev => ({
      ...prev,
      [id]: {
        config: { ...(prev[id]?.config ?? {}), [key]: '' },
        dirty: true,
      },
    }))
  }

  const removeConfigKey = (id: string, key: string) => {
    setEdits(prev => {
      const cfg = { ...(prev[id]?.config ?? {}) }
      delete cfg[key]
      return { ...prev, [id]: { config: cfg, dirty: true } }
    })
  }

  return (
    <div className="plugins-overlay" onClick={e => { if (e.target === e.currentTarget) onClose() }}>
      <div className="plugins-modal">
        <div className="plugins-modal-header">
          <h2>Plugins</h2>
          <button className="plugins-close" onClick={onClose} aria-label="Close">×</button>
        </div>

        {error && (
          <div className="plugins-error">{error}</div>
        )}

        {loading && <div className="plugins-loading">Loading…</div>}

        <div className="plugins-list">
          {plugins.map(plugin => {
            // Use plugin.id as the React key and the map key for edits.
            const edit = edits[plugin.id] ?? { config: {}, dirty: false }
            const schema = plugin.configSchema ?? {}
            const schemaKeys = Object.keys(schema)
            const isLegacy = plugin.protocol < 1
            const hasSchema = schemaKeys.length > 0

            // Keys in config that are NOT in schema → "Advanced" section.
            const advancedKeys = Object.keys(edit.config).filter(k => !schema[k])

            return (
              <div key={plugin.id} className="plugin-card">
                <div className="plugin-card-header">
                  <div className="plugin-name-row">
                    {/* Display the human-readable basename, not the full path */}
                    <span className="plugin-name">{plugin.name}</span>
                    <span className={`plugin-status ${plugin.running ? 'running' : 'stopped'}`}>
                      {plugin.running ? '● running' : '○ stopped'}
                    </span>
                  </div>
                  <div className="plugin-controls">
                    <label className="plugin-toggle">
                      <input
                        type="checkbox"
                        checked={plugin.enabled}
                        onChange={e => handleToggle(plugin.id, e.target.checked)}
                      />
                      {plugin.enabled ? 'enabled' : 'disabled'}
                    </label>
                    <button
                      className="plugin-restart-btn"
                      onClick={() => handleRestart(plugin.id)}
                      title="Restart plugin"
                    >
                      restart
                    </button>
                  </div>
                </div>

                {plugin.description && (
                  <p className="plugin-description">{plugin.description}</p>
                )}

                {isLegacy ? (
                  <p className="plugin-legacy-note">(legacy — not configurable)</p>
                ) : (
                  <>
                    {hasSchema && (
                      <div className="plugin-schema-fields">
                        {schemaKeys.map(key => {
                          const field: ConfigField = schema[key]
                          return (
                            <div key={key} className="plugin-field">
                              <label className="plugin-field-label">
                                <span className="plugin-field-name">{key}</span>
                                <input
                                  type="text"
                                  className="plugin-field-input"
                                  value={edit.config[key] ?? field.default ?? ''}
                                  placeholder={field.default ?? ''}
                                  onChange={e => setConfigField(plugin.id, key, e.target.value)}
                                />
                              </label>
                              {field.description && (
                                <span className="plugin-field-desc">{field.description}</span>
                              )}
                            </div>
                          )
                        })}
                      </div>
                    )}

                    {/* Advanced section: keys not in schema */}
                    <details className="plugin-advanced">
                      <summary>Advanced</summary>
                      <div className="plugin-advanced-fields">
                        {advancedKeys.map(key => (
                          <div key={key} className="plugin-field plugin-field-advanced">
                            <span className="plugin-field-name">{key}</span>
                            <input
                              type="text"
                              className="plugin-field-input"
                              value={edit.config[key] ?? ''}
                              onChange={e => setConfigField(plugin.id, key, e.target.value)}
                            />
                            <button
                              className="plugin-remove-key"
                              onClick={() => removeConfigKey(plugin.id, key)}
                              title="Remove key"
                            >
                              ×
                            </button>
                          </div>
                        ))}
                        <button
                          className="plugin-add-key"
                          onClick={() => addConfigKey(plugin.id)}
                        >
                          + add key
                        </button>
                      </div>
                    </details>

                    <div className="plugin-actions">
                      <button
                        className="plugin-save-btn"
                        disabled={!edit.dirty}
                        onClick={() => handleSave(plugin.id)}
                      >
                        Save
                      </button>
                      <button
                        className="plugin-revert-btn"
                        disabled={!edit.dirty}
                        onClick={() => handleRevert(plugin.id, plugin)}
                      >
                        Revert
                      </button>
                      <span className="plugin-restart-note">restarts plugin on save</span>
                    </div>
                  </>
                )}
              </div>
            )
          })}

          {!loading && plugins.length === 0 && (
            <div className="plugins-empty">No plugins discovered.</div>
          )}
        </div>
      </div>
    </div>
  )
}
