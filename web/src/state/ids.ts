// Typed references ("decision 275", "run 311"): the label an ID chip shows and the
// parser that reads one back. Pure: no DOM, no store.

/** The things a person can point at by number. */
export type IdKind = 'item' | 'request' | 'decision' | 'question' | 'run' | 'topic'

export interface ParsedRef {
  kind: IdKind
  id: number
}

/** The label a reference shows: "decision 275". */
export function refLabel(kind: IdKind, id: number): string {
  return `${kind} ${id}`
}

const KINDS: readonly IdKind[] = ['item', 'request', 'decision', 'question', 'run', 'topic']
const TYPED = new RegExp(`^(${KINDS.join('|')})\\s+(\\d+)$`, 'i')
const BARE = /^#(\d+)$/

const valid = (n: number) => Number.isSafeInteger(n) && n > 0

function isKind(kind: string | undefined): kind is IdKind {
  return (KINDS as readonly string[]).includes(kind ?? '')
}

/**
 * Reads a reference back: "decision 275" gives { kind: 'decision', id: 275 }, and a
 * bare "#82" reads as an item. Anything else gives null.
 */
export function parseRef(text: string): ParsedRef | null {
  const t = text.trim()
  const bare = BARE.exec(t)
  if (bare) {
    const id = Number(bare[1])
    return valid(id) ? { kind: 'item', id } : null
  }
  const typed = TYPED.exec(t)
  if (typed) {
    const kind = typed[1]?.toLowerCase()
    const id = Number(typed[2])
    if (isKind(kind) && valid(id)) return { kind, id }
  }
  return null
}
