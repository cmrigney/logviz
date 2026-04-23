# logviz — Plan

A Wails desktop app that sits in a Unix pipeline, passes logs through to the terminal unchanged, and mirrors them into a filterable GUI.

## Usage modes

Two invocation modes, so stderr is never lost:

1. **Pipe mode** — `producer | logviz`
   - Reads `os.Stdin`, tees every line to `os.Stdout`.
   - Users who want stderr too run `producer 2>&1 | logviz` (documented in README).
   - Limitation: once streams are merged with `2>&1`, logviz cannot tell stdout from stderr — every line is tagged `stdin`.

2. **Wrap mode** — `logviz -- producer arg1 arg2 ...`
   - logviz spawns `producer` as a subprocess via `exec.Cmd` with separate pipes for stdout/stderr.
   - Each line is tagged with its source stream (`stdout` / `stderr`) and forwarded to logviz's own stdout/stderr respectively so downstream tools still see the correct streams.
   - Exit code of the wrapped process is propagated when logviz exits.
   - This is the recommended mode for anything that logs to stderr.

Detection: if `os.Args` contains `--` treat as wrap mode, else if stdin is a pipe (non-TTY via `os.Stdin.Stat()`) treat as pipe mode, else show a "launch me from a pipeline" message in the window.

## Architecture

Single Go binary produced by `wails build`.

**Backend (Go)**
- `main.go`: mode detection, stdin/subprocess plumbing, Wails bootstrap.
- `app.go`: `App` struct holding a buffered channel `chan LogLine` where `LogLine = { Seq uint64; Source string; Text string; TimeNs int64 }`.
- One reader goroutine per source stream (stdin OR subprocess stdout + subprocess stderr). Each reads with `bufio.Scanner` (buffer bumped to 1 MB for long lines), writes the raw line back to its corresponding FD on the host terminal, then pushes a `LogLine` into the channel.
- One emitter goroutine drains the channel in ~50 ms batches and calls `runtime.EventsEmit(ctx, "log:batch", lines)`. Batching is mandatory — per-line IPC chokes the webview past ~1 k lines/s.
- Backpressure: channel is bounded (e.g. 8192). On overflow, drop oldest and increment a `dropped` counter exposed via a second event `log:dropped`.
- logviz's own diagnostics go to `os.Stderr` with a `[logviz]` prefix so they're distinguishable from passthrough.

**Frontend (React + TypeScript)**
- `wails init -n logviz -t react-ts`.
- State:
  - Ring buffer (cap 100 k lines, drop oldest) held in a `useRef` — not React state, to avoid re-renders on every append.
  - Filter state (`useState`): query string, regex toggle, case-sensitive toggle, source toggles (stdout/stderr/stdin), paused flag, autoscroll flag.
  - A `tick` counter bumped on each incoming batch drives a `useMemo` that recomputes the filtered view.
- UI:
  - Top toolbar: search input, regex toggle, case toggle, source chips (stdout/stderr/stdin with per-source counts), pause/resume, clear, autoscroll toggle, total count, dropped-lines badge.
  - Virtualized list via `@tanstack/react-virtual` — plain DOM dies past ~10 k rows.
  - Each row: monospace text, source-colored left border (stdout=neutral, stderr=red, stdin=blue), match highlighting via regex split.
- Wire-up: on mount, `EventsOn("log:batch", lines => ring.push(...lines); setTick(t=>t+1))` and `EventsOn("log:dropped", n => setDropped(d => d+n))`.

## Install / distribution

- macOS `.app` bundles don't inherit stdin cleanly, so the pipeline use case requires the raw binary on `$PATH`.
- `wails build` output is `build/bin/logviz` (binary) + `build/bin/logviz.app` (bundle).
- README documents: `cp build/bin/logviz /usr/local/bin/` or symlink.

## Steps

1. `wails init -n logviz -t react-ts` in `/Users/codyrigney/Development/logviz`.
2. Edit `main.go`: add mode detection, stdin reader (pipe mode), subprocess spawner with stdout+stderr readers (wrap mode), channel plumbing, before `wails.Run`.
3. Edit `app.go`: add `LogLine` type, batched emitter goroutine using `runtime.EventsEmit`, dropped-count tracking.
4. Build the React UI:
   - Ring-buffer hook (`useRingBuffer`).
   - Filter hook (`useFilter`) with regex/case/source options.
   - `<Toolbar />`, `<LogList />` (virtualized), `<LogRow />` components.
   - Event-subscription effect in `App.tsx`.
5. `wails build`. Test matrix:
   - `yes "hello $(date)" | head -n 1000 | ./build/bin/logviz`
   - `./build/bin/logviz -- sh -c 'for i in $(seq 1 500); do echo out-$i; echo err-$i 1>&2; done'`
   - High-rate producer to exercise backpressure + dropped counter.
6. README with install + both usage modes + `2>&1` tip for pipe mode.

## Open questions

- **Backpressure UX**: dropped-count badge (proposed) vs. blocking the producer. Badge is safer — blocking can wedge a slow UI into stalling the upstream app.
- **Level parsing**: skip for MVP — regex filter covers `INFO|WARN|ERROR` well enough. Level chips can come later.
- **Persistence / export**: out of scope for MVP. Could add "save filtered view to file" later.
