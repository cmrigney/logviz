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

type plugin struct {
	name  string
	ch    chan LogLine
	cmd   *exec.Cmd
	stdin io.WriteCloser
	done  chan struct{} // closed when the subprocess exits
}

type pluginManager struct {
	plugins []*plugin
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

type pluginSpec struct {
	name string
	cmd  string
	args []string
}

func startPlugins(ctx context.Context) *pluginManager {
	specs := discoverPlugins()
	ctx, cancel := context.WithCancel(ctx)
	pm := &pluginManager{cancel: cancel}
	for _, spec := range specs {
		p, err := launchPlugin(ctx, spec, &pm.wg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[plugin:%s] failed to start: %v\n", spec.name, err)
			continue
		}
		pm.plugins = append(pm.plugins, p)
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
		spec, ok := resolvePlugin(filepath.Join(dir, name), name, e)
		if !ok {
			continue
		}
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
		name:  spec.name,
		ch:    make(chan LogLine, 256),
		cmd:   cmd,
		stdin: stdin,
		done:  make(chan struct{}),
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

func writePlugin(ctx context.Context, p *plugin) {
	defer p.stdin.Close()
	enc := json.NewEncoder(p.stdin)
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.done:
			return
		case line := <-p.ch:
			if err := enc.Encode(line); err != nil {
				return
			}
		}
	}
}

func (pm *pluginManager) dispatch(line LogLine) {
	if pm == nil {
		return
	}
	for _, p := range pm.plugins {
		select {
		case p.ch <- line:
		default:
		}
	}
}

func (pm *pluginManager) stop() {
	if pm == nil {
		return
	}
	for _, p := range pm.plugins {
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
