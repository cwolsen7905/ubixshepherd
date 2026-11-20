// Prefixed IDs in prose ("Q-196", "D-370"): which thing each prefix names, and where a
// piece of text holds one. Pure: no DOM, no router.

/** The kinds of thing a prefix can open, named as the inspector's `open=` kinds are. */
export type PrefixedKind = 'question' | 'decision' | 'next' | 'run' | 'lane' | 'topic'

const PREFIX: Record<string, PrefixedKind> = {
  Q: 'question',
  D: 'decision',
  N: 'next',
  R: 'run',
  L: 'lane',
  T: 'topic',
}

export interface PrefixedId {
  kind: PrefixedKind
  id: number
  /** The text as written, "Q-196". */
  text: string
}

// A prefix letter, a hyphen, digits; not glued to a word or number on either side, so
// "MQ-5", "Q-5x", "D-1-2" and "UTF-8" stay text.
const ID = /(?<![\w\-/.:=#@])([QDNRLT])-(\d+)(?![\w-]|\/|\.\w)/g

/** Reads one ID from the whole of `text`, or null. */
export function parsePrefixedId(text: string): PrefixedId | null {
  const m = /^([QDNRLT])-(\d+)$/.exec(text)
  return m ? make(m[1] as string, m[2] as string, text) : null
}

function make(letter: string, digits: string, text: string): PrefixedId | null {
  const kind = PREFIX[letter]
  const id = Number(digits)
  if (!kind || !Number.isSafeInteger(id) || id <= 0) return null
  return { kind, id, text }
}

/** Splits `text` into plain strings and the IDs found between them, in order. */
export function splitPrefixedIds(text: string): (string | PrefixedId)[] {
  const out: (string | PrefixedId)[] = []
  let at = 0
  for (const m of text.matchAll(ID)) {
    const found = make(m[1] as string, m[2] as string, m[0])
    if (!found) continue
    if (m.index > at) out.push(text.slice(at, m.index))
    out.push(found)
    at = m.index + m[0].length
  }
  if (at < text.length) out.push(text.slice(at))
  return out
}
