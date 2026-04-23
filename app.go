package main

import (
	"context"
	"sync"
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

const (
	maxBacklog = 100_000 // cap the replay buffer; drop oldest past this
	emitChunk  = 2048    // max lines per emitted batch
)

type App struct {
	ctx       context.Context
	startInfo startInfo

	mu  sync.Mutex
	buf []LogLine

	dropped atomic.Uint64
	seq     atomic.Uint64

	readyCh   chan struct{}
	readyOnce sync.Once
}

type startInfo struct {
	Mode        string   `json:"mode"`
	Command     []string `json:"command,omitempty"`
	Passthrough bool     `json:"passthrough"`
}

func NewApp(info startInfo) *App {
	return &App{
		startInfo: info,
		readyCh:   make(chan struct{}),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go a.emitLoop()
}

func (a *App) StartInfo() startInfo {
	return a.startInfo
}

// Ready is called by the frontend once its EventsOn listeners are registered.
// Until this fires, the emit loop buffers logs instead of firing events that
// would be dropped by an un-mounted webview.
func (a *App) Ready() {
	a.readyOnce.Do(func() { close(a.readyCh) })
}

func (a *App) push(source, text string) {
	line := LogLine{
		Seq:    a.seq.Add(1),
		Source: source,
		Text:   text,
		TimeMs: time.Now().UnixMilli(),
	}
	a.mu.Lock()
	a.buf = append(a.buf, line)
	if over := len(a.buf) - maxBacklog; over > 0 {
		a.dropped.Add(uint64(over))
		a.buf = append(a.buf[:0], a.buf[over:]...)
	}
	a.mu.Unlock()
}

func (a *App) drain() []LogLine {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.buf) == 0 {
		return nil
	}
	out := a.buf
	a.buf = nil
	return out
}

func (a *App) emitBatch(batch []LogLine) {
	for i := 0; i < len(batch); i += emitChunk {
		end := i + emitChunk
		if end > len(batch) {
			end = len(batch)
		}
		runtime.EventsEmit(a.ctx, "log:batch", batch[i:end])
	}
}

func (a *App) emitLoop() {
	select {
	case <-a.readyCh:
	case <-a.ctx.Done():
		return
	}

	// Flush any backlog accumulated before the UI was ready.
	if batch := a.drain(); batch != nil {
		a.emitBatch(batch)
	}

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	lastDropped := uint64(0)
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if batch := a.drain(); batch != nil {
				a.emitBatch(batch)
			}
			if d := a.dropped.Load(); d != lastDropped {
				runtime.EventsEmit(a.ctx, "log:dropped", d)
				lastDropped = d
			}
		}
	}
}
