// What a desk turn from the inspector is about. The first version writes it into the
// message as a reference line; N22 adds a structured `about` to POST /v1/desk/turn, so
// `About` already has that shape (an entity kind and the ids) and only `withReference`
// goes away then.
import type { TopicKind, TopicView } from '../model/topic'

/** The kinds the composer speaks about: what the inspector can hold. */
export type AboutKind = 'lane' | 'run' | 'decision' | 'question' | 'request' | 'artifact' | 'topic'

/** The entity the open inspector item is. `label` is the words the person knows it by. */
export interface About {
  kind: AboutKind
  ids: number[]
  label?: string
}

/** True for an inspector kind the composer can speak about. */
export function isAboutKind(k: string): k is AboutKind {
  return k in WORDS
}

const WORDS: Record<AboutKind, string> = {
  lane: 'lane', run: 'run', decision: 'decision', question: 'question', request: 'request', artifact: 'artifact', topic: 'topic',
}

/** "feat/export-csv, decision 5": what the composer's foot says the message is about. */
export function aboutLine(a: About): string {
  const ref = `${WORDS[a.kind]} ${a.ids.join(', ')}`
  return a.label ? `${a.label} (${ref})` : ref
}

/** The turn text: a reference line the desk reads, a blank line, then the person's words. */
export function withReference(a: About, text: string): string {
  return `About ${aboutLine(a)}:\n\n${text.trim()}`
}

/** The link kind a topic holds the entity under; null for a kind topics do not link. */
export function linkKind(k: AboutKind): TopicKind | null {
  switch (k) {
    case 'lane':
    case 'run':
    case 'decision':
    case 'request':
      return k
    case 'question':
    case 'artifact':
    case 'topic':
      return null
  }
}

/**
 * The open topic that links the entity, which the turn is filed under; undefined when none
 * does, and the desk files it as it does any turn. The first match in the daemon's order.
 */
export function topicFor(a: About, topics: TopicView[]): number | undefined {
  if (a.kind === 'topic') return a.ids[0]
  const kind = linkKind(a.kind)
  if (!kind) return undefined
  const refs = new Set(a.ids.map(String))
  return topics.find((t) => t.status === 'open' && t.links.some((l) => l.kind === kind && refs.has(l.ref)))?.id
}
