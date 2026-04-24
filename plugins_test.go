package main

import (
	"context"
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
	pm := &pluginManager{cancel: cancel}
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
