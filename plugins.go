package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// pluginManifest is the optional <name>.json sibling file describing a plugin.
type pluginManifest struct {
	Protocol     int                    `json:"protocol"`
	Description  string                 `json:"description"`
	ConfigSchema map[string]ConfigField `json:"configSchema"`
}

// pluginSpec holds all information needed to launch a plugin.
type pluginSpec struct {
	name         string
	cmd          string
	args         []string
	enabled      bool
	config       map[string]string
	protocol     int // 0 = legacy bare LogLine, 1 = envelope
	description  string
	configSchema map[string]ConfigField
}

// plugin is a running plugin subprocess.
type plugin struct {
	name       string
	ch         chan LogLine
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	done       chan struct{} // closed when the subprocess exits
	warnedDrop sync.Once
	protocol   int
	config     map[string]string
}

// pluginManager owns all running plugins and their discovery specs.
type pluginManager struct {
	mu       sync.Mutex
	plugins  []*plugin
	allSpecs []pluginSpec // all discovered specs, including disabled ones
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	ctx      context.Context
}

func startPlugins(ctx context.Context) *pluginManager {
	specs := discoverPlugins()
	// Apply persisted config.
	cfg, err := loadPluginConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[plugin-config] load error: %v\n", err)
		cfg, _ = loadPluginConfig() // retry or use zero value
		if cfg == nil {
			cfg = &pluginConfigFile{Version: 1, Plugins: make(map[string]pluginConfigEntry)}
		}
	}
	for i, spec := range specs {
		entry := cfg.entryFor(spec.name)
		specs[i].enabled = entry.Enabled
		specs[i].config = entry.Config
	}

	ctx, cancel := context.WithCancel(ctx)
	pm := &pluginManager{
		cancel:   cancel,
		ctx:      ctx,
		allSpecs: specs,
	}
	for _, spec := range specs {
		if !spec.enabled {
			continue
		}
		p, err := launchPlugin(ctx, spec, &pm.wg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[plugin:%s] failed to start: %v\n", spec.name, err)
			continue
		}
		pm.mu.Lock()
		pm.plugins = append(pm.plugins, p)
		pm.mu.Unlock()
	}
	return pm
}

func discoverPlugins() []pluginSpec {
	var specs []pluginSpec
	if home, err := os.UserHomeDir(); err == nil {
		specs = append(specs, scanPluginDir(filepath.Join(home, ".config", "logviz", "plugins"))...)
	}
	specs = append(specs, scanPluginDir("plugins")...)
	return specs
}

func scanPluginDir(dir string) []pluginSpec {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var specs []pluginSpec
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || e.IsDir() {
			continue
		}
		// Skip manifest files — they are handled by resolvePlugin.
		if strings.HasSuffix(name, ".json") {
			continue
		}
		spec, ok := resolvePlugin(filepath.Join(dir, name), name, e)
		if !ok {
			continue
		}
		// Attempt to load sibling manifest.
		manifestPath := filepath.Join(dir, name+".json")
		if data, err := os.ReadFile(manifestPath); err == nil {
			var m pluginManifest
			if jsonErr := json.Unmarshal(data, &m); jsonErr == nil {
				spec.protocol = m.Protocol
				spec.description = m.Description
				spec.configSchema = m.ConfigSchema
			} else {
				fmt.Fprintf(os.Stderr, "[plugin:%s] invalid manifest: %v\n", name, jsonErr)
			}
		}
		// Default enabled = true (overridden by persisted config in startPlugins).
		spec.enabled = true
		specs = append(specs, spec)
	}
	return specs
}

func resolvePlugin(path, name string, e os.DirEntry) (pluginSpec, bool) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".js", ".mjs", ".cjs":
		return pluginSpec{name: name, cmd: "node", args: []string{path}}, true
	}
	info, err := e.Info()
	if err != nil {
		return pluginSpec{}, false
	}
	if info.Mode()&0o111 != 0 {
		return pluginSpec{name: name, cmd: path}, true
	}
	return pluginSpec{}, false
}

func launchPlugin(ctx context.Context, spec pluginSpec, wg *sync.WaitGroup) (*plugin, error) {
	cmd := exec.CommandContext(ctx, spec.cmd, spec.args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	cmd.Stdout = io.Discard
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	p := &plugin{
		name:     spec.name,
		ch:       make(chan LogLine, 1024),
		cmd:      cmd,
		stdin:    stdin,
		done:     make(chan struct{}),
		protocol: spec.protocol,
		config:   spec.config,
	}
	wg.Add(3)
	go func() {
		defer wg.Done()
		pumpStderr(p.name, stderr)
	}()
	go func() {
		defer wg.Done()
		writePlugin(ctx, p)
	}()
	go func() {
		defer wg.Done()
		err := cmd.Wait()
		close(p.done)
		if err != nil && ctx.Err() == nil {
			fmt.Fprintf(os.Stderr, "[plugin:%s] exited: %v\n", p.name, err)
		}
	}()
	return p, nil
}

func pumpStderr(name string, r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		fmt.Fprintf(os.Stderr, "[plugin:%s] %s\n", name, scanner.Text())
	}
}

// envelopeHello is the first message sent to protocol ≥ 1 plugins.
type envelopeHello struct {
	Type     string            `json:"type"`
	Protocol int               `json:"protocol"`
	Plugin   string            `json:"plugin"`
	Config   map[string]string `json:"config"`
}

// envelopeLog wraps a LogLine for protocol ≥ 1 plugins.
type envelopeLog struct {
	Type string  `json:"type"`
	Line LogLine `json:"line"`
}

func writePlugin(ctx context.Context, p *plugin) {
	defer p.stdin.Close()
	enc := json.NewEncoder(p.stdin)

	// For protocol ≥ 1, send the hello message first.
	if p.protocol >= 1 {
		cfg := p.config
		if cfg == nil {
			cfg = make(map[string]string)
		}
		hello := envelopeHello{
			Type:     "hello",
			Protocol: p.protocol,
			Plugin:   p.name,
			Config:   cfg,
		}
		if err := enc.Encode(hello); err != nil {
			return
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.done:
			return
		case line := <-p.ch:
			var err error
			if p.protocol >= 1 {
				err = enc.Encode(envelopeLog{Type: "log", Line: line})
			} else {
				err = enc.Encode(line)
			}
			if err != nil {
				return
			}
		}
	}
}

func (pm *pluginManager) dispatchBatch(lines []LogLine) {
	if pm == nil {
		return
	}
	pm.mu.Lock()
	plugins := pm.plugins
	pm.mu.Unlock()
	for _, p := range plugins {
		for _, line := range lines {
			select {
			case p.ch <- line:
			default:
				p.warnedDrop.Do(func() {
					fmt.Fprintf(os.Stderr, "[plugin:%s] dropping lines (channel full); further drops silent\n", p.name)
				})
			}
		}
	}
}

func (pm *pluginManager) stop() {
	if pm == nil {
		return
	}
	pm.mu.Lock()
	plugins := pm.plugins
	pm.mu.Unlock()
	for _, p := range plugins {
		_ = p.stdin.Close()
	}
	done := make(chan struct{})
	go func() {
		pm.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
	pm.cancel()
}

// stopPlugin closes a single plugin's stdin and waits up to 2 s for it to exit.
// If the process has not exited by then it is forcibly killed, ensuring the old
// process is always gone before a replacement is launched.
func (pm *pluginManager) stopPlugin(p *plugin) {
	_ = p.stdin.Close()
	select {
	case <-p.done:
		// exited cleanly
	case <-time.After(2 * time.Second):
		// Timed out — kill the process so it cannot outlive this call.
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		// Wait for done to be closed (the Wait goroutine will close it promptly
		// after Kill returns an error, which is expected and ignored there).
		<-p.done
	}
}

// restartPlugin stops a named plugin (if running) and relaunches it if enabled.
// The spec in allSpecs is used (which reflects any config changes).
func (pm *pluginManager) restartPlugin(name string) error {
	pm.mu.Lock()
	// Find the spec.
	var spec *pluginSpec
	for i := range pm.allSpecs {
		if pm.allSpecs[i].name == name {
			spec = &pm.allSpecs[i]
			break
		}
	}
	if spec == nil {
		pm.mu.Unlock()
		return fmt.Errorf("plugin %q not found", name)
	}
	specCopy := *spec

	// Find and remove existing running plugin.
	var existing *plugin
	newPlugins := pm.plugins[:0:len(pm.plugins)]
	for _, p := range pm.plugins {
		if p.name == name {
			existing = p
		} else {
			newPlugins = append(newPlugins, p)
		}
	}
	pm.plugins = newPlugins
	pm.mu.Unlock()

	// Stop the existing plugin outside the lock.
	if existing != nil {
		pm.stopPlugin(existing)
	}

	// Relaunch if enabled.
	if specCopy.enabled {
		p, err := launchPlugin(pm.ctx, specCopy, &pm.wg)
		if err != nil {
			return fmt.Errorf("relaunch plugin %q: %w", name, err)
		}
		pm.mu.Lock()
		pm.plugins = append(pm.plugins, p)
		pm.mu.Unlock()
	}
	return nil
}

// isRunning reports whether a plugin with the given name is currently running.
func (pm *pluginManager) isRunning(name string) bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for _, p := range pm.plugins {
		if p.name == name {
			select {
			case <-p.done:
				return false
			default:
				return true
			}
		}
	}
	return false
}
