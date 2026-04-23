# logviz

A desktop log viewer that sits inside a Unix pipeline. Logs stream through to the terminal unchanged **and** into a filterable GUI.

```
producer | logviz                # pipe mode
logviz -- producer arg1 arg2     # wrap mode (keeps stdout / stderr separate)
logviz -q -- producer arg1       # -q / --no-passthrough: GUI only, skip tee
```

## Features

- **Terminal passthrough** — every line is echoed to the same FD it would have gone to anyway, so downstream tools and your scrollback keep working.
- **Two modes**:
  - **Pipe**: reads `stdin`, tees to `stdout`. Use `producer 2>&1 | logviz` to capture stderr too (merged stream — the UI tags it as `stdin`).
  - **Wrap**: spawns the producer as a subprocess, keeps stdout and stderr on separate pipes, forwards each to the matching FD on the host terminal, and tags lines in the UI by source.
- **Live filtering** — substring or regex, case toggle, per-source chips (stdout / stderr / stdin) with live counts, match highlighting.
- **Virtualized list** via `@tanstack/react-virtual` — handles hundreds of thousands of lines without choking.
- **Pause / clear / autoscroll**, plus a dropped-lines badge when the producer outruns the UI.
- **`-q` / `--no-passthrough`** — suppress the terminal tee and only send logs to the GUI.

## Requirements

- [Wails v2](https://wails.io/docs/gettingstarted/installation) CLI (`wails version` ≥ 2.10)
- Go 1.21+
- Node 18+

## Build

```sh
wails build
```

Output: `build/bin/logviz.app` (macOS bundle) containing the executable at
`build/bin/logviz.app/Contents/MacOS/logviz`.

Piping `|` into a `.app` bundle does not work — you need to invoke the raw executable inside the bundle, which is what the symlink step below sets up.

## Install (link onto `$PATH`)

```sh
ln -s "$PWD/build/bin/logviz.app/Contents/MacOS/logviz" /usr/local/bin/logviz
```

Then from any shell:

```sh
tail -f /var/log/system.log | logviz
logviz -- npm run dev
logviz -- ping -c 20 1.1.1.1
```

To uninstall: `rm /usr/local/bin/logviz`.

## Plugins

Drop an executable into `~/.config/logviz/plugins/` or `./plugins/` (relative to where logviz is launched). Both dirs are scanned at startup and merged. Each plugin is spawned as a long-running subprocess and receives every log line as NDJSON on stdin:

```
{"seq":1,"source":"stdout","text":"hello","timeMs":1713900000000}
```

Node scripts (`.js` / `.mjs` / `.cjs`) are first-class — no shebang or `chmod +x` needed. Other extensions must have the executable bit set. Plugin `stderr` is tagged with `[plugin:<name>]` and forwarded to logviz's stderr.

Minimal example (`~/.config/logviz/plugins/save-errors.js`):

```js
const fs = require('node:fs');
const readline = require('node:readline');
readline.createInterface({ input: process.stdin }).on('line', (l) => {
  const log = JSON.parse(l);
  if (log.source === 'stderr') fs.appendFileSync('/tmp/errors.log', log.text + '\n');
});
```

See [`plugins/`](./plugins) in this repo for more examples.

## Development

```sh
wails dev
```

Starts Vite with hot reload. The Go devtools server runs at http://localhost:34115. Note: `wails dev` launches the app without a piped stdin, so it starts in `idle` mode and shows usage hints — test pipe / wrap modes against the built binary instead.

## Project layout

```
main.go              # mode detection, stdin reader, subprocess spawner
app.go               # LogLine type, batched event emitter, dropped-line counter
frontend/src/
  App.tsx            # UI: toolbar, virtualized list, filter logic
  App.css
  style.css
```
