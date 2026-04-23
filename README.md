# logviz

A desktop log viewer that sits inside a Unix pipeline. Logs stream through to the terminal unchanged **and** into a filterable GUI.

```
producer | logviz                # pipe mode
logviz -- producer arg1 arg2     # wrap mode (keeps stdout / stderr separate)
```

## Features

- **Terminal passthrough** — every line is echoed to the same FD it would have gone to anyway, so downstream tools and your scrollback keep working.
- **Two modes**:
  - **Pipe**: reads `stdin`, tees to `stdout`. Use `producer 2>&1 | logviz` to capture stderr too (merged stream — the UI tags it as `stdin`).
  - **Wrap**: spawns the producer as a subprocess, keeps stdout and stderr on separate pipes, forwards each to the matching FD on the host terminal, and tags lines in the UI by source.
- **Live filtering** — substring or regex, case toggle, per-source chips (stdout / stderr / stdin) with live counts, match highlighting.
- **Virtualized list** via `@tanstack/react-virtual` — handles hundreds of thousands of lines without choking.
- **Pause / clear / autoscroll**, plus a dropped-lines badge when the producer outruns the UI.

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
