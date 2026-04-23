const ANSI = /\x1b\[[0-9;]*m/g
const ISO = /\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z?/g
const CLOCK = /\b\d{1,2}:\d{2}:\d{2}(?:\.\d+)?\b/g
const UUID = /\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b/g
const IPV4 = /\b(?:\d{1,3}\.){3}\d{1,3}\b/g
const HEX = /\b[0-9a-fA-F]{8,}\b/g
const NUMS = /\d{3,}/g
const WS = /\s+/g

export function signatureOf(source: string, text: string): string {
  let s = text.replace(ANSI, '')
  s = s.replace(ISO, '<T>')
  s = s.replace(CLOCK, '<T>')
  s = s.replace(UUID, '<U>')
  s = s.replace(IPV4, '<IP>')
  s = s.replace(HEX, '<H>')
  s = s.replace(NUMS, '<N>')
  s = s.replace(WS, ' ').trim()
  return source + '|' + s
}
