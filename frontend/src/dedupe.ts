import { signatureOf } from './signature'

export type ServerLogLine = { seq: number; source: string; text: string; timeMs: number }
export type LogLine = ServerLogLine & { sig: string }

type SigEntry = { firstSeq: number; count: number }

export class DedupeIndex {
  private sigMap = new Map<string, SigEntry>()

  attach(raw: ServerLogLine): LogLine {
    const sig = signatureOf(raw.source, raw.text)
    const entry = this.sigMap.get(sig)
    if (entry) entry.count++
    else this.sigMap.set(sig, { firstSeq: raw.seq, count: 1 })
    return { ...raw, sig }
  }

  evict(lines: LogLine[]): void {
    for (const e of lines) {
      const entry = this.sigMap.get(e.sig)
      if (!entry) continue
      if (entry.firstSeq === e.seq || entry.count <= 1) {
        this.sigMap.delete(e.sig)
      } else {
        entry.count--
      }
    }
  }

  clear(): void {
    this.sigMap.clear()
  }

  isRepresentative(line: LogLine): boolean {
    const entry = this.sigMap.get(line.sig)
    return !entry || entry.firstSeq === line.seq
  }

  countFor(line: LogLine): number {
    return this.sigMap.get(line.sig)?.count ?? 1
  }
}
