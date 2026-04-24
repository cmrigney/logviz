import { ReactNode } from 'react'

/**
 * Renders a pre-parsed JSON value as a syntax-highlighted <pre> block.
 * Colours:
 *   keys        #7ec8e3  light blue
 *   strings     #a8d8a8  light green
 *   numbers/bool/null  #e8b86d  amber
 *   punctuation #888
 */

type JsonValue =
  | { kind: 'object'; entries: [string, JsonValue][] }
  | { kind: 'array';  items: JsonValue[] }
  | { kind: 'string'; value: string }
  | { kind: 'number'; value: number }
  | { kind: 'boolean'; value: boolean }
  | { kind: 'null' }

// Fix 7: renamed from parse() to toJsonValue() to avoid ambiguity with JSON.parse
function toJsonValue(raw: unknown): JsonValue {
  if (raw === null) return { kind: 'null' }
  if (typeof raw === 'boolean') return { kind: 'boolean', value: raw }
  if (typeof raw === 'number') return { kind: 'number', value: raw }
  if (typeof raw === 'string') return { kind: 'string', value: raw }
  if (Array.isArray(raw)) return { kind: 'array', items: raw.map(toJsonValue) }
  const entries = Object.entries(raw as Record<string, unknown>).map<[string, JsonValue]>(
    ([k, v]) => [k, toJsonValue(v)]
  )
  return { kind: 'object', entries }
}

const INDENT = '  '
// Fix 8: depth cap
const MAX_DEPTH = 8

// Fix 5: only styled tokens get span wrappers; plain strings (newlines, indents)
// are returned as bare strings so React emits them as text nodes.
function renderValue(val: JsonValue, depth: number, isLast: boolean, keyNodes?: ReactNode[]): ReactNode[] {
  const indentClose = INDENT.repeat(depth - 1)
  const comma = isLast ? '' : ','

  // Fix 8: emit collapse indicator past max depth
  if (depth > MAX_DEPTH) {
    return [
      keyNodes,
      <span className="jp-punct">…</span>,
      comma ? <span className="jp-punct">{comma}</span> : null,
      '\n',
    ]
  }

  // Fix 10: trailing colon+space — colon is punctuation, space is bare text
  const keySpan = (k: string): ReactNode[] => [
    <span className="jp-punct">"</span>,
    <span className="jp-key">{k}</span>,
    <span className="jp-punct">":</span>,
    ' ',
  ]

  if (val.kind === 'null') {
    return [keyNodes, <span className="jp-keyword">null</span>, comma ? <span className="jp-punct">{comma}</span> : null, '\n']
  }
  if (val.kind === 'boolean') {
    return [keyNodes, <span className="jp-keyword">{String(val.value)}</span>, comma ? <span className="jp-punct">{comma}</span> : null, '\n']
  }
  if (val.kind === 'number') {
    return [keyNodes, <span className="jp-keyword">{String(val.value)}</span>, comma ? <span className="jp-punct">{comma}</span> : null, '\n']
  }
  if (val.kind === 'string') {
    return [
      keyNodes,
      <span className="jp-punct">"</span>,
      <span className="jp-string">{val.value}</span>,
      <span className="jp-punct">"</span>,
      comma ? <span className="jp-punct">{comma}</span> : null,
      '\n',
    ]
  }
  if (val.kind === 'array') {
    if (val.items.length === 0) {
      return [keyNodes, <span className="jp-punct">{'[]'}</span>, comma ? <span className="jp-punct">{comma}</span> : null, '\n']
    }
    const children: ReactNode[] = []
    val.items.forEach((item, i) => {
      const last = i === val.items.length - 1
      const childIndent = INDENT.repeat(depth)  // bare text node
      children.push(childIndent, ...renderValue(item, depth + 1, last))
    })
    return [
      keyNodes,
      <span className="jp-punct">{'['}</span>,
      '\n',
      ...children,
      indentClose,
      <span className="jp-punct">{']'}</span>,
      comma ? <span className="jp-punct">{comma}</span> : null,
      '\n',
    ]
  }
  // object
  if (val.entries.length === 0) {
    return [keyNodes, <span className="jp-punct">{'{}'}</span>, comma ? <span className="jp-punct">{comma}</span> : null, '\n']
  }
  const children: ReactNode[] = []
  val.entries.forEach(([k, v], i) => {
    const last = i === val.entries.length - 1
    const childIndent = INDENT.repeat(depth)  // bare text node
    // Fix 10: keyNode uses separate span for colon and bare ' ' after it
    const keyNode: ReactNode[] = [childIndent, ...keySpan(k)]
    children.push(...renderValue(v, depth + 1, last, keyNode))
  })
  return [
    keyNodes,
    <span className="jp-punct">{'{'}</span>,
    '\n',
    ...children,
    indentClose,
    <span className="jp-punct">{'}'}</span>,
    comma ? <span className="jp-punct">{comma}</span> : null,
    '\n',
  ]
}

// Fix 1: accepts pre-parsed value as a prop — no JSON.parse inside
export function JsonPretty({ parsed }: { parsed: unknown }) {
  const val = toJsonValue(parsed)
  const nodes = renderValue(val, 1, true)

  // Fix 4: flat(2) — actual nesting depth of output array is at most 2
  // Fix 5: only span-wrapped nodes get key; bare strings are emitted as-is
  return (
    <pre className="jp-pre">
      {(nodes.flat(2) as ReactNode[]).map((n, i) =>
        n == null ? null : (typeof n === 'string' ? n : <span key={i}>{n}</span>)
      )}
    </pre>
  )
}

/** Returns the parsed value if text is valid JSON object/array, otherwise null. */
export function tryParseJson(text: string): unknown {
  if (text.length < 2) return null
  const first = text.trimStart()[0]
  if (first !== '{' && first !== '[') return null
  try {
    return JSON.parse(text)
  } catch {
    return null
  }
}
