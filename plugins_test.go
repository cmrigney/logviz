package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestPluginReceivesLogLinesViaNode(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not on PATH")
	}

	dir := t.TempDir()
	outfile := filepath.Join(dir, "count")
	script := filepath.Join(dir, "count.js")

	// Plugin: count NDJSON lines on stdin, write the current count to outfile
	// after each. Using path from argv so the test can assert it.
	js := `const readline = require('node:readline');
const fs = require('node:fs');
const out = process.argv[2];
let n = 0;
readline.createInterface({ input: process.stdin }).on('line', () => {
  n++;
  fs.writeFileSync(out, String(n));
});`
	if err := os.WriteFile(script, []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	p, err := launchPlugin(ctx, pluginSpec{
		name: "count.js",
		cmd:  "node",
		args: []string{script, outfile},
	}, &wg)
	if err != nil {
		t.Fatalf("launchPlugin: %v", err)
	}

	const N = 50
	for i := 0; i < N; i++ {
		p.ch <- LogLine{Seq: uint64(i + 1), Source: "test", Text: "hello", TimeMs: 1}
	}

	// Wait until the plugin has written N, or fail after 2s.
	deadline := time.Now().Add(2 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(outfile)
		if err == nil {
			got = string(data)
			if got == "50" {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got != "50" {
		t.Fatalf("expected plugin to have processed 50 lines, outfile=%q", got)
	}

	// Signal EOF and wait for plugin to exit.
	_ = p.stdin.Close()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("plugin did not exit within 2s of stdin close")
	}
}

func TestPluginReceivesBacklogBeforeStartup(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not on PATH")
	}

	dir := t.TempDir()
	outfile := filepath.Join(dir, "out")
	script := filepath.Join(dir, "tee.js")
	js := `const fs = require('node:fs');
const rl = require('node:readline').createInterface({ input: process.stdin });
rl.on('line', (l) => {
  const log = JSON.parse(l);
  fs.appendFileSync(process.argv[2], log.text + '\n');
});`
	if err := os.WriteFile(script, []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp(startInfo{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pm := &pluginManager{cancel: cancel, ctx: ctx}
	p, err := launchPlugin(ctx, pluginSpec{
		name: "tee.js",
		cmd:  "node",
		args: []string{script, outfile},
	}, &pm.wg)
	if err != nil {
		t.Fatalf("launchPlugin: %v", err)
	}
	pm.plugins = append(pm.plugins, p)
	app.plugins = pm

	// Pre-Ready: these land in app.buf; plugins see nothing until a drain/emit.
	app.push("stdout", "pre-1")
	app.push("stdout", "pre-2")

	// Mirror emitLoop's post-Ready drain + per-batch dispatch.
	pm.dispatchBatch(app.drain())

	// Post-Ready: new lines also accumulate in buf until the next tick drains.
	app.push("stdout", "post-1")
	app.push("stdout", "post-2")
	pm.dispatchBatch(app.drain())

	want := "pre-1\npre-2\npost-1\npost-2\n"
	deadline := time.Now().Add(2 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(outfile); err == nil {
			got = string(data)
			if got == want {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got != want {
		t.Fatalf("plugin missed backlog or reordered\nwant:\n%s\ngot:\n%s", want, got)
	}

	_ = p.stdin.Close()
	pm.wg.Wait()
}

func TestResolvePluginJS(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.js", "b.mjs", "c.cjs"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("// plugin\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	specs := scanPluginDir(dir)
	if len(specs) != 3 {
		t.Fatalf("expected 3 specs, got %d", len(specs))
	}
	for _, s := range specs {
		if s.cmd != "node" {
			t.Errorf("expected node launcher for %s, got %q", s.name, s.cmd)
		}
	}
}

func TestResolvePluginExecutable(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(execPath, []byte("#!/bin/sh\ncat >/dev/null\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	nonExec := filepath.Join(dir, "readme.txt")
	if err := os.WriteFile(nonExec, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	specs := scanPluginDir(dir)
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec (exec only), got %d", len(specs))
	}
	if specs[0].name != "run.sh" {
		t.Errorf("unexpected plugin: %+v", specs[0])
	}
}

func TestScanMissingDir(t *testing.T) {
	specs := scanPluginDir(filepath.Join(t.TempDir(), "does-not-exist"))
	if specs != nil {
		t.Fatalf("expected nil for missing dir, got %v", specs)
	}
}

// ── New tests ────────────────────────────────────────────────────────────────

// TestPluginManifestLoading verifies that scanPluginDir picks up the sibling
// .json manifest file and populates protocol and configSchema.
func TestPluginManifestLoading(t *testing.T) {
	dir := t.TempDir()

	// Write a JS plugin file.
	if err := os.WriteFile(filepath.Join(dir, "myplugin.js"), []byte("// plugin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Write its manifest.
	manifest := pluginManifest{
		Protocol:    1,
		Description: "test plugin",
		ConfigSchema: map[string]ConfigField{
			"host": {Type: "string", Default: "localhost", Description: "hostname"},
		},
	}
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, "myplugin.js.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Also write a legacy plugin with no manifest.
	if err := os.WriteFile(filepath.Join(dir, "legacy.js"), []byte("// legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	specs := scanPluginDir(dir)
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(specs))
	}

	// Find myplugin.js spec.
	var mySpec, legacySpec *pluginSpec
	for i := range specs {
		if specs[i].name == "myplugin.js" {
			mySpec = &specs[i]
		} else if specs[i].name == "legacy.js" {
			legacySpec = &specs[i]
		}
	}
	if mySpec == nil {
		t.Fatal("myplugin.js spec not found")
	}
	if mySpec.protocol != 1 {
		t.Errorf("expected protocol 1, got %d", mySpec.protocol)
	}
	if mySpec.description != "test plugin" {
		t.Errorf("unexpected description: %q", mySpec.description)
	}
	if len(mySpec.configSchema) != 1 {
		t.Errorf("expected 1 schema field, got %d", len(mySpec.configSchema))
	}
	if f, ok := mySpec.configSchema["host"]; !ok || f.Default != "localhost" {
		t.Errorf("unexpected schema field: %+v", mySpec.configSchema)
	}

	// Legacy plugin must have protocol 0 and no schema.
	if legacySpec == nil {
		t.Fatal("legacy.js spec not found")
	}
	if legacySpec.protocol != 0 {
		t.Errorf("legacy plugin should have protocol 0, got %d", legacySpec.protocol)
	}
	if len(legacySpec.configSchema) != 0 {
		t.Errorf("legacy plugin should have no schema, got %v", legacySpec.configSchema)
	}
}

// TestPluginConfigFileRoundtrip verifies save → load round-trips correctly and
// that a missing file returns an empty struct without error.
func TestPluginConfigFileRoundtrip(t *testing.T) {
	dir := t.TempDir()

	// Point configPath() at a temp file so the test never touches the real
	// user config directory on any platform (XDG_CONFIG_HOME is Linux-only).
	configPathOverride = filepath.Join(dir, "plugins.json")
	t.Cleanup(func() { configPathOverride = "" })

	// Missing file → empty struct, no error.
	cfg, err := loadPluginConfig()
	if err != nil {
		t.Fatalf("loadPluginConfig on missing file: %v", err)
	}
	if len(cfg.Plugins) != 0 {
		t.Errorf("expected empty plugins map, got %v", cfg.Plugins)
	}
	if cfg.Version != 1 {
		t.Errorf("expected version 1, got %d", cfg.Version)
	}

	// Save a config.
	original := &pluginConfigFile{
		Version: 1,
		Plugins: map[string]pluginConfigEntry{
			"myplugin.js": {
				Enabled: true,
				Config:  map[string]string{"host": "example.com", "port": "9000"},
			},
			"other.js": {
				Enabled: false,
				Config:  map[string]string{},
			},
		},
	}
	if err := savePluginConfig(original); err != nil {
		t.Fatalf("savePluginConfig: %v", err)
	}

	// Verify permissions.
	p := configPath()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat config file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected 0o600, got %o", info.Mode().Perm())
	}

	// Load back and compare.
	loaded, err := loadPluginConfig()
	if err != nil {
		t.Fatalf("loadPluginConfig after save: %v", err)
	}
	if loaded.Version != original.Version {
		t.Errorf("version mismatch: got %d", loaded.Version)
	}
	if len(loaded.Plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(loaded.Plugins))
	}
	entry := loaded.Plugins["myplugin.js"]
	if !entry.Enabled {
		t.Error("myplugin.js should be enabled")
	}
	if entry.Config["host"] != "example.com" {
		t.Errorf("host mismatch: %q", entry.Config["host"])
	}
	other := loaded.Plugins["other.js"]
	if other.Enabled {
		t.Error("other.js should be disabled")
	}
}

// TestPluginReceivesHelloAndLogs verifies that a protocol-1 plugin receives the
// hello message with config and subsequently receives log lines wrapped in
// the envelope format.
func TestPluginReceivesHelloAndLogs(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not on PATH")
	}

	dir := t.TempDir()
	helloFile := filepath.Join(dir, "hello.json")
	countFile := filepath.Join(dir, "count")
	script := filepath.Join(dir, "proto1.js")

	// Protocol-1 plugin: write hello config to helloFile, count log lines to countFile.
	js := `const readline = require('node:readline');
const fs = require('node:fs');
const helloFile = process.argv[2];
const countFile = process.argv[3];
let count = 0;
readline.createInterface({ input: process.stdin }).on('line', (raw) => {
  let msg;
  try { msg = JSON.parse(raw); } catch { return; }
  if (msg.type === 'hello') {
    fs.writeFileSync(helloFile, JSON.stringify(msg.config));
    return;
  }
  if (msg.type === 'log') {
    count++;
    fs.writeFileSync(countFile, String(count));
  }
});`
	if err := os.WriteFile(script, []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup

	cfg := map[string]string{"serverName": "test-server", "mcpPath": "~/.test/mcp.json"}
	p, err := launchPlugin(ctx, pluginSpec{
		name:     "proto1.js",
		cmd:      "node",
		args:     []string{script, helloFile, countFile},
		protocol: 1,
		config:   cfg,
	}, &wg)
	if err != nil {
		t.Fatalf("launchPlugin: %v", err)
	}

	const N = 10
	for i := 0; i < N; i++ {
		p.ch <- LogLine{Seq: uint64(i + 1), Source: "stdout", Text: "line", TimeMs: 1}
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(countFile)
		if err == nil && string(data) == "10" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Check hello payload.
	helloData, err := os.ReadFile(helloFile)
	if err != nil {
		t.Fatalf("hello file not written: %v", err)
	}
	var gotConfig map[string]string
	if err := json.Unmarshal(helloData, &gotConfig); err != nil {
		t.Fatalf("unmarshal hello config: %v", err)
	}
	if gotConfig["serverName"] != "test-server" {
		t.Errorf("serverName mismatch: %q", gotConfig["serverName"])
	}

	// Check count.
	countData, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatalf("count file not written: %v", err)
	}
	if string(countData) != "10" {
		t.Errorf("expected 10 log lines, got %q", string(countData))
	}

	_ = p.stdin.Close()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("plugin did not exit within 2s")
	}
}

// TestLegacyPluginStillWorks confirms that protocol-0 plugins with no manifest
// still receive bare LogLine NDJSON (no envelope wrapper).
func TestLegacyPluginStillWorks(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not on PATH")
	}

	dir := t.TempDir()
	outfile := filepath.Join(dir, "out")
	script := filepath.Join(dir, "legacy.js")

	// Legacy plugin: parse bare LogLine, write text to outfile.
	js := `const readline = require('node:readline');
const fs = require('node:fs');
const out = process.argv[2];
readline.createInterface({ input: process.stdin }).on('line', (raw) => {
  let log;
  try { log = JSON.parse(raw); } catch { return; }
  // A bare LogLine has 'text' at top level (not wrapped in 'line').
  if (log.text !== undefined) {
    fs.appendFileSync(out, log.text + '\n');
  }
});`
	if err := os.WriteFile(script, []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup

	// protocol=0 (default), no manifest.
	p, err := launchPlugin(ctx, pluginSpec{
		name:     "legacy.js",
		cmd:      "node",
		args:     []string{script, outfile},
		protocol: 0,
	}, &wg)
	if err != nil {
		t.Fatalf("launchPlugin: %v", err)
	}

	p.ch <- LogLine{Seq: 1, Source: "stdout", Text: "hello-legacy", TimeMs: 1}

	deadline := time.Now().Add(2 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(outfile)
		if err == nil && len(data) > 0 {
			got = string(data)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got != "hello-legacy\n" {
		t.Errorf("legacy plugin got wrong output: %q", got)
	}

	_ = p.stdin.Close()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("plugin did not exit within 2s")
	}
}

// TestSetPluginConfigRestartsPlugin verifies that calling App.SetPluginConfig
// restarts the plugin and the new hello message carries the new config.
func TestSetPluginConfigRestartsPlugin(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not on PATH")
	}

	dir := t.TempDir()
	// The plugin writes each hello config it receives (appends) to a file.
	helloLog := filepath.Join(dir, "hellos.ndjson")
	// PID file: write process PID on hello.
	pidFile := filepath.Join(dir, "pid")
	script := filepath.Join(dir, "configtest.js")

	js := `const readline = require('node:readline');
const fs = require('node:fs');
const helloLog = process.argv[2];
const pidFile = process.argv[3];
readline.createInterface({ input: process.stdin }).on('line', (raw) => {
  let msg;
  try { msg = JSON.parse(raw); } catch { return; }
  if (msg.type === 'hello') {
    fs.appendFileSync(helloLog, JSON.stringify({config: msg.config, pid: process.pid}) + '\n');
    fs.writeFileSync(pidFile, String(process.pid));
  }
});`
	if err := os.WriteFile(script, []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}

	// Build the app and pluginManager manually.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app := NewApp(startInfo{})
	app.ctx = ctx

	pm := &pluginManager{cancel: cancel, ctx: ctx}
	app.plugins = pm

	initialCfg := map[string]string{"key": "initial"}
	spec := pluginSpec{
		name:     "configtest.js",
		cmd:      "node",
		args:     []string{script, helloLog, pidFile},
		protocol: 1,
		config:   initialCfg,
		enabled:  true,
	}
	pm.allSpecs = []pluginSpec{spec}

	p, err := launchPlugin(ctx, spec, &pm.wg)
	if err != nil {
		t.Fatalf("launchPlugin: %v", err)
	}
	pm.mu.Lock()
	pm.plugins = append(pm.plugins, p)
	pm.mu.Unlock()

	// Wait for first hello.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil && len(data) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	firstPIDData, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("pid file not written after initial launch: %v", err)
	}
	firstPID := string(firstPIDData)

	// Call SetPluginConfig with a new config.
	// Point configPath() at the temp dir so it never touches real user config.
	configPathOverride = filepath.Join(dir, "plugins.json")
	t.Cleanup(func() { configPathOverride = "" })
	if err := app.SetPluginConfig("configtest.js", map[string]string{"key": "updated"}); err != nil {
		t.Fatalf("SetPluginConfig: %v", err)
	}

	// Wait for second hello.
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidFile)
		if err == nil && string(data) != firstPID {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	secondPIDData, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("pid file missing after restart: %v", err)
	}
	if string(secondPIDData) == firstPID {
		t.Error("plugin PID did not change — plugin was not restarted")
	}

	// Check the hello log: last hello should carry the new config.
	helloData, err := os.ReadFile(helloLog)
	if err != nil {
		t.Fatalf("hello log missing: %v", err)
	}
	lines := splitNDJSON(helloData)
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 hello messages, got %d: %s", len(lines), helloData)
	}
	var lastHello struct {
		Config map[string]string `json:"config"`
		PID    int               `json:"pid"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &lastHello); err != nil {
		t.Fatalf("parse last hello: %v", err)
	}
	if lastHello.Config["key"] != "updated" {
		t.Errorf("expected config key=updated, got %q", lastHello.Config["key"])
	}

	cancel()
	pm.wg.Wait()
}

// splitNDJSON splits a newline-delimited JSON byte slice into non-empty lines.
func splitNDJSON(data []byte) []string {
	var lines []string
	start := 0
	for i, b := range data {
		if b == '\n' {
			line := string(data[start:i])
			if line != "" {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(data) {
		line := string(data[start:])
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
