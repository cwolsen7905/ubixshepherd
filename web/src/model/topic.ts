// Conversation topics (docs/topics.md): a name for a matter the thread is about, and what
// belongs to it. The daemon derives what waits and when it was last active; this holds
// the filter the thread applies and the words the rail shows.
import type { ThreadItem } from '../state/desk'

export type TopicKind = 'turn' | 'feed' | 'next_item' | 'lane' | 'run' | 'decision' | 'request'

export interface TopicLinkView {
  kind: TopicKind
  ref: string
  by: 'desk' | 'shepherd' | 'person'
  label?: string
  state?: string
  waits?: boolean
  time: string
}

export interface TopicView {
  id: number
  title: string
  status: 'open' | 'cleared'
  summary?: string
  source: string
  created: string
  updated: string
  cleared_at?: string
  activity: string
  waits: number
  settled?: boolean
  reopen?: boolean
  links: TopicLinkView[]
}

export interface TopicList {
  topics: TopicView[]
}

export interface TopicUpdateBody {
  title?: string
  summary?: string
  status?: 'open' | 'cleared'
  merge_into?: number
  link?: { kind: TopicKind; ref: string }
}

/** The topic the thread is filtered to: `topic=N` in the hash's query; null when none. */
export function readTopicFilter(hash: string): number | null {
  const q = hash.split('?')[1] ?? ''
  const n = Number(new URLSearchParams(q).get('topic'))
  return Number.isInteger(n) && n > 0 ? n : null
}

/** The hash with the topic filter set, or removed for null; other parts are kept. */
export function withTopicFilter(hash: string, id: number | null): string {
  const [path = '#/chat', q = ''] = hash.split('?')
  const params = new URLSearchParams(q)
  if (id === null) params.delete('topic')
  else params.set('topic', String(id))
  const rest = params.toString()
  return rest ? `${path}?${rest}` : path
}

/** What a feed row's `ref` names, from the row's event: null when it names nothing a topic holds. */
export function feedRefKind(event: string | undefined): TopicKind | null {
  if (!event) return null
  if (event.startsWith('decision')) return 'decision'
  if (event.startsWith('request')) return 'request'
  if (event.startsWith('run')) return 'run'
  if (event.startsWith('lane')) return 'lane'
  return null
}

/** The thing keys (`kind:ref`) a topic holds, for testing rows against it. */
export function memberKeys(t: Pick<TopicView, 'links'>): Set<string> {
  return new Set(t.links.map((l) => `${l.kind}:${l.ref}`))
}

/** Whether a thread row belongs to the topic: a turn or feed row it is linked to, or a row about a lane, run, decision or request it holds. */
export function inTopic(it: ThreadItem, members: Set<string>): boolean {
  if (it.type === 'desk') return it.e.turn !== undefined && members.has(`turn:${it.e.turn}`)
  if (members.has(`feed:${it.f.id}`)) return true
  const kind = feedRefKind(it.f.event)
  return kind !== null && it.f.ref !== undefined && members.has(`${kind}:${it.f.ref}`)
}

/** The open topics, in the order the daemon gave (active and idle alike). */
export function activeTopics(list: TopicView[]): TopicView[] {
  return list.filter((t) => t.status === 'open')
}

/** How recent a touch keeps an open topic active; older and quiet, it goes under Idle. */
export const ACTIVE_WINDOW_MS = 2 * 60 * 60 * 1000

/**
 * Whether an open topic is active: something under it waits on the person, a run in it is
 * running, or it was touched within the window. An undated topic counts as active, so
 * nothing is hidden for want of a time. The window is exclusive: exactly its age is idle.
 */
export function isActive(t: TopicView, now: number): boolean {
  if (t.waits > 0) return true
  if (t.links.some((l) => l.kind === 'run' && l.state === 'running')) return true
  const at = Date.parse(t.activity)
  return Number.isNaN(at) || now - at < ACTIVE_WINDOW_MS
}

/**
 * Splits the open topics, keeping the daemon's order, into active and idle. The selected
 * topic is always active, so a filter never hides its own chip.
 */
export function splitTopics(list: TopicView[], now: number, selected: number | null): { active: TopicView[]; idle: TopicView[] } {
  const active: TopicView[] = []
  const idle: TopicView[] = []
  for (const t of activeTopics(list)) (isActive(t, now) || t.id === selected ? active : idle).push(t)
  return { active, idle }
}

/** A remembered open/closed flag; false when storage is missing or unreadable. */
export function readFlag(key: string): boolean {
  try {
    return window.localStorage.getItem(key) === '1'
  } catch {
    return false
  }
}

export function writeFlag(key: string, on: boolean): void {
  try {
    window.localStorage.setItem(key, on ? '1' : '0')
  } catch {
    // Private mode or a full quota: the flag lasts only as long as the component.
  }
}

/** The one-line state of a topic for its row. */
export function topicState(t: TopicView): string {
  if (t.status === 'cleared') return t.reopen ? 'cleared, something needs you' : 'cleared'
  if (t.waits > 0) return `waits on you: ${t.waits}`
  if (t.settled) return 'settled?'
  return ''
}

const KIND_NAMES: Record<TopicKind, string> = {
  turn: 'Turn', feed: 'Row', next_item: 'Next up', lane: 'Lane', run: 'Run', decision: 'Decision', request: 'Request',
}
export const kindName = (k: TopicKind) => KIND_NAMES[k] ?? k
