import { ReactNode } from 'react'

/**
 * Renders a parsed JSON value as a syntax-highlighted <pre> block.
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

function parse(raw: unknown): JsonValue {
  if (raw === null) return { kind: 'null' }
  if (typeof raw === 'boolean') return { kind: 'boolean', value: raw }
  if (typeof raw === 'number') return { kind: 'number', value: raw }
  if (typeof raw === 'string') return { kind: 'string', value: raw }
  if (Array.isArray(raw)) return { kind: 'array', items: raw.map(parse) }
  // object
  const entries = Object.entries(raw as Record<string, unknown>).map<[string, JsonValue]>(
    ([k, v]) => [k, parse(v)]
  )
  return { kind: 'object', entries }
}

const INDENT = '  '

function renderValue(val: JsonValue, depth: number, isLast: boolean, keyNodes?: ReactNode[]): ReactNode[] {
  const indentClose = INDENT.repeat(depth - 1)
  const comma = isLast ? '' : ','

  const punct = (s: string) => <span className="jp-punct">{s}</span>
  const kw = (s: string) => <span className="jp-keyword">{s}</span>

  if (val.kind === 'null') {
    return [keyNodes, kw('null'), comma ? punct(comma) : null, '\n']
  }
  if (val.kind === 'boolean') {
    return [keyNodes, kw(String(val.value)), comma ? punct(comma) : null, '\n']
  }
  if (val.kind === 'number') {
    return [keyNodes, kw(String(val.value)), comma ? punct(comma) : null, '\n']
  }
  if (val.kind === 'string') {
    return [
      keyNodes,
      punct('"'),
      <span className="jp-string">{val.value}</span>,
      punct('"'),
      comma ? punct(comma) : null,
      '\n',
    ]
  }
  if (val.kind === 'array') {
    if (val.items.length === 0) {
      return [keyNodes, punct('[]'), comma ? punct(comma) : null, '\n']
    }
    const children: ReactNode[] = []
    val.items.forEach((item, i) => {
      const last = i === val.items.length - 1
      const childIndent = INDENT.repeat(depth)
      children.push(
        childIndent,
        ...renderValue(item, depth + 1, last)
      )
    })
    return [
      keyNodes,
      punct('['),
      '\n',
      ...children,
      indentClose,
      punct(']'),
      comma ? punct(comma) : null,
      '\n',
    ]
  }
  // object
  if (val.entries.length === 0) {
    return [keyNodes, punct('{}'), comma ? punct(comma) : null, '\n']
  }
  const children: ReactNode[] = []
  val.entries.forEach(([k, v], i) => {
    const last = i === val.entries.length - 1
    const childIndent = INDENT.repeat(depth)
    const keyNode: ReactNode[] = [
      childIndent,
      punct('"'),
      <span className="jp-key">{k}</span>,
      punct('": '),
    ]
    children.push(...renderValue(v, depth + 1, last, keyNode))
  })
  return [
    keyNodes,
    punct('{'),
    '\n',
    ...children,
    indentClose,
    punct('}'),
    comma ? punct(comma) : null,
    '\n',
  ]
}

export function JsonPretty({ text }: { text: string }) {
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    // Shouldn't happen — callers should gate on tryParseJson
    return <>{text}</>
  }

  const val = parse(parsed)
  const nodes = renderValue(val, 1, true)

  return (
    <pre className="jp-pre">
      {nodes.flat(Infinity as never).map((n, i) =>
        n == null ? null : <span key={i}>{n}</span>
      )}
    </pre>
  )
}

/** Returns the parsed value if text is valid JSON, otherwise null. */
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
