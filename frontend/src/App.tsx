import { useEffect, useMemo, useRef, useState, ReactNode } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { EventsOn, EventsOff } from '../wailsjs/runtime/runtime'
import { StartInfo, Ready } from '../wailsjs/go/main/App'
import './App.css'

type LogLine = { seq: number; source: string; text: string; timeMs: number }

type StartInfoT = { mode: string; command?: string[] }

type Matcher =
  | { kind: 'regex'; re: RegExp }
  | { kind: 'substr'; needle: string; caseSensitive: boolean }
  | null

const MAX_LINES = 100_000
const SOURCES = ['stdout', 'stderr', 'stdin'] as const

function App() {
  const ringRef = useRef<LogLine[]>([])
  const [tick, setTick] = useState(0)

  const [query, setQuery] = useState('')
  const [regex, setRegex] = useState(false)
  const [caseSensitive, setCaseSensitive] = useState(false)
  const [paused, setPaused] = useState(false)
  const [autoscroll, setAutoscroll] = useState(true)
  const [sources, setSources] = useState<Record<string, boolean>>({
    stdout: true, stderr: true, stdin: true,
  })
  const [dropped, setDropped] = useState(0)
  const [startInfo, setStartInfo] = useState<StartInfoT>({ mode: 'idle' })

  const pausedRef = useRef(paused)
  pausedRef.current = paused

  useEffect(() => {
    StartInfo().then(info => setStartInfo(info as StartInfoT)).catch(() => {})
    EventsOn('log:batch', (...args: unknown[]) => {
      const batch = args[0] as LogLine[]
      if (pausedRef.current) return
      const ring = ringRef.current
      for (const line of batch) ring.push(line)
      if (ring.length > MAX_LINES) ring.splice(0, ring.length - MAX_LINES)
      setTick(t => t + 1)
    })
    EventsOn('log:dropped', (...args: unknown[]) => {
      setDropped(args[0] as number)
    })
    // Signal Go that listeners are registered so it flushes its backlog.
    Ready().catch(() => {})
    return () => {
      EventsOff('log:batch')
      EventsOff('log:dropped')
    }
  }, [])

  const [matcher, matcherError] = useMemo<[Matcher, string | null]>(() => {
    if (!query) return [null, null]
    if (regex) {
      try {
        return [{ kind: 'regex', re: new RegExp(query, caseSensitive ? 'g' : 'gi') }, null]
      } catch (e) {
        return [null, (e as Error).message]
      }
    }
    return [{ kind: 'substr', needle: caseSensitive ? query : query.toLowerCase(), caseSensitive }, null]
  }, [query, regex, caseSensitive])

  const filtered = useMemo(() => {
    void tick
    const ring = ringRef.current
    const out: LogLine[] = []
    for (const line of ring) {
      if (!sources[line.source]) continue
      if (matcher) {
        if (matcher.kind === 'regex') {
          matcher.re.lastIndex = 0
          if (!matcher.re.test(line.text)) continue
        } else {
          const t = matcher.caseSensitive ? line.text : line.text.toLowerCase()
          if (!t.includes(matcher.needle)) continue
        }
      }
      out.push(line)
    }
    return out
  }, [tick, matcher, sources])

  const sourceCounts = useMemo(() => {
    void tick
    const c: Record<string, number> = {}
    for (const l of ringRef.current) c[l.source] = (c[l.source] || 0) + 1
    return c
  }, [tick])

  const parentRef = useRef<HTMLDivElement>(null)
  const virtualizer = useVirtualizer({
    count: filtered.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 20,
    overscan: 30,
  })

  useEffect(() => {
    if (autoscroll && !paused && filtered.length > 0) {
      virtualizer.scrollToIndex(filtered.length - 1, { align: 'end' })
    }
  }, [filtered.length, autoscroll, paused, virtualizer])

  const clear = () => {
    ringRef.current = []
    setTick(t => t + 1)
  }

  const toggleSource = (s: string) =>
    setSources(v => ({ ...v, [s]: !v[s] }))

  const totalLines = ringRef.current.length
  const showEmpty = totalLines === 0

  return (
    <div className="app">
      <header className="toolbar">
        <input
          className={`search ${matcherError ? 'invalid' : ''}`}
          placeholder={regex ? 'regex…' : 'filter…'}
          value={query}
          onChange={e => setQuery(e.target.value)}
          title={matcherError ?? ''}
          autoComplete="off"
          autoCorrect="off"
          autoCapitalize="off"
          spellCheck={false}
        />
        <label>
          <input type="checkbox" checked={regex} onChange={e => setRegex(e.target.checked)} />
          regex
        </label>
        <label>
          <input type="checkbox" checked={caseSensitive} onChange={e => setCaseSensitive(e.target.checked)} />
          case
        </label>
        <div className="chips">
          {SOURCES.map(s => (
            <button
              key={s}
              className={`chip chip-${s} ${sources[s] ? 'on' : 'off'}`}
              onClick={() => toggleSource(s)}
              title={`toggle ${s}`}
            >
              {s} {sourceCounts[s] ?? 0}
            </button>
          ))}
        </div>
        <button onClick={() => setPaused(p => !p)}>
          {paused ? 'resume' : 'pause'}
        </button>
        <button onClick={clear}>clear</button>
        <label>
          <input type="checkbox" checked={autoscroll} onChange={e => setAutoscroll(e.target.checked)} />
          autoscroll
        </label>
        <span className="stats">
          <span className="mode-badge">{startInfo.mode}</span>
          {' '}
          {filtered.length.toLocaleString()} / {totalLines.toLocaleString()}
          {dropped > 0 && <> · <span className="dropped">{dropped.toLocaleString()} dropped</span></>}
        </span>
      </header>

      {showEmpty ? (
        <div className="empty-state">
          <div>no logs yet</div>
          <div>
            pipe mode: <code>producer | logviz</code> (use <code>2&gt;&amp;1 |</code> to include stderr)
          </div>
          <div>
            wrap mode: <code>logviz -- producer args…</code> (keeps stdout / stderr separate)
          </div>
        </div>
      ) : (
        <div ref={parentRef} className="log-list">
          <div style={{ height: virtualizer.getTotalSize(), position: 'relative' }}>
            {virtualizer.getVirtualItems().map(v => {
              const line = filtered[v.index]
              return (
                <div
                  key={v.key}
                  className={`log-row log-row-${line.source}`}
                  style={{
                    position: 'absolute',
                    top: 0,
                    left: 0,
                    right: 0,
                    transform: `translateY(${v.start}px)`,
                    height: v.size,
                  }}
                >
                  <span className="seq">{line.seq}</span>
                  <span className="log-text">
                    <Highlighted text={line.text} matcher={matcher} />
                  </span>
                </div>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}

function Highlighted({ text, matcher }: { text: string; matcher: Matcher }) {
  if (!matcher) return <>{text}</>
  const parts: ReactNode[] = []

  if (matcher.kind === 'regex') {
    const re = matcher.re
    re.lastIndex = 0
    let last = 0
    let m: RegExpExecArray | null
    let i = 0
    while ((m = re.exec(text)) !== null) {
      if (m.index > last) parts.push(text.slice(last, m.index))
      parts.push(<mark key={i++}>{m[0]}</mark>)
      last = m.index + m[0].length
      if (m[0].length === 0) re.lastIndex++
    }
    if (last < text.length) parts.push(text.slice(last))
  } else {
    const src = matcher.caseSensitive ? text : text.toLowerCase()
    const q = matcher.needle
    if (q.length === 0) return <>{text}</>
    let last = 0
    let idx = src.indexOf(q)
    let i = 0
    while (idx !== -1) {
      if (idx > last) parts.push(text.slice(last, idx))
      parts.push(<mark key={i++}>{text.slice(idx, idx + q.length)}</mark>)
      last = idx + q.length
      idx = src.indexOf(q, last)
    }
    if (last < text.length) parts.push(text.slice(last))
  }
  return <>{parts}</>
}

export default App
