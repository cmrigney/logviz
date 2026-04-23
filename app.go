package main

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type LogLine struct {
	Seq    uint64 `json:"seq"`
	Source string `json:"source"`
	Text   string `json:"text"`
	TimeMs int64  `json:"timeMs"`
}

type App struct {
	ctx       context.Context
	lines     chan LogLine
	dropped   atomic.Uint64
	seq       atomic.Uint64
	startInfo startInfo
}

type startInfo struct {
	Mode    string   `json:"mode"`
	Command []string `json:"command,omitempty"`
}

func NewApp(lines chan LogLine, info startInfo) *App {
	return &App{lines: lines, startInfo: info}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go a.emitLoop()
}

func (a *App) StartInfo() startInfo {
	return a.startInfo
}

func (a *App) push(source, text string) {
	line := LogLine{
		Seq:    a.seq.Add(1),
		Source: source,
		Text:   text,
		TimeMs: time.Now().UnixMilli(),
	}
	select {
	case a.lines <- line:
	default:
		a.dropped.Add(1)
	}
}

func (a *App) emitLoop() {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	batch := make([]LogLine, 0, 256)
	lastDropped := uint64(0)
	for {
		select {
		case <-a.ctx.Done():
			return
		case line, ok := <-a.lines:
			if !ok {
				if len(batch) > 0 {
					runtime.EventsEmit(a.ctx, "log:batch", batch)
				}
				return
			}
			batch = append(batch, line)
			if len(batch) >= 1024 {
				runtime.EventsEmit(a.ctx, "log:batch", batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				runtime.EventsEmit(a.ctx, "log:batch", batch)
				batch = batch[:0]
			}
			if d := a.dropped.Load(); d != lastDropped {
				runtime.EventsEmit(a.ctx, "log:dropped", d)
				lastDropped = d
			}
		}
	}
}
