import type { WaitingItem } from '../api/types'
import { age } from '../format'
import { boardItems, type BoardItem } from '../model/board'
import { href } from '../router'
import { attentionOf, waitingOnYou, type StatusInput } from '../state/attention'
import type { GlyphTone } from '../ui/StatusGlyph'

/** The four status counts, in the sidebar's order. */
export type CountKind = 'running' | 'waiting' | 'broken' | 'routing'

export const COUNT_KINDS: readonly CountKind[] = ['running', 'waiting', 'broken', 'routing']

/** The popover's most items; past it a "See all" link. */
export const POP_MAX = 5

export interface PopItem {
  key: string
  glyph: string
  tone: GlyphTone
  /** The title cut at a whole word. */
  title: string
  /** The full title, for the tooltip. */
  full: string
  /** The lane, faint. */
  where: string
  /** Relative age, right-aligned. */
  age: string
  href: string
}

export interface CountSpec {
  glyph: string
  tone: GlyphTone
  /** The count's name after the number: "2 running". */
  noun: string
  /** The popover title when nothing is in the list. */
  none: string
  /** The popover title with n items; "{n}" stands for the number. */
  title: string
  /** The list the count opens: Board or Inbox. */
  to: () => string
  toName: string
}

export const SPEC: Record<CountKind, CountSpec> = {
  running: { glyph: '●', tone: 'live', noun: 'running', none: 'Nothing running', title: '{n} running', to: () => href.board(), toName: 'Board' },
  waiting: { glyph: '?', tone: 'warn', noun: 'waiting on you', none: 'Nothing waiting on you', title: '{n} waiting on you', to: () => href.inbox(), toName: 'Inbox' },
  broken: { glyph: '✗', tone: 'bad', noun: 'broken', none: 'Nothing broken', title: '{n} broken', to: () => href.board(), toName: 'Board' },
  routing: { glyph: '!', tone: 'warn', noun: 'needing routing', none: 'Nothing to route', title: '{n} to route', to: () => href.inbox(), toName: 'Inbox' },
}

/** `text` cut at a whole word to at most `max` characters, with an ellipsis when cut. */
export function cutWord(text: string, max = 44): string {
  if (text.length <= max) return text
  const head = text.slice(0, max + 1)
  const at = head.lastIndexOf(' ')
  return `${(at > max / 2 ? head.slice(0, at) : text.slice(0, max)).replace(/[\s,;:.-]+$/, '')}…`
}

const WAIT_GLYPH: Record<WaitingItem['kind'], [string, GlyphTone]> = {
  decision: ['?', 'warn'],
  question: ['?', 'warn'],
  request: ['!', 'warn'],
  mr: ['◆', 'warn'],
  ship: ['◆', 'warn'],
  next: ['≡', 'warn'],
  blocked: ['✗', 'bad'],
  scope: ['!', 'warn'],
}

function waitingHref(it: WaitingItem): string {
  if (it.decision_id) return href.decisions(it.decision_id)
  if (it.next_id) return href.next(it.next_id)
  if (it.lane_id) return href.lane(it.lane_id)
  return it.kind === 'question' ? href.chat() : href.inbox()
}

function fromWaiting(it: WaitingItem, now: number): PopItem {
  const [glyph, tone] = WAIT_GLYPH[it.kind]
  const at = it.since ? now - Date.parse(it.since) : NaN
  return {
    key: `${it.kind}:${it.decision_id ?? it.request_id ?? it.next_id ?? it.mr ?? it.question_id ?? it.title}`,
    glyph,
    tone,
    title: cutWord(it.title),
    full: it.title,
    where: it.lane ?? it.repo ?? '',
    age: Number.isFinite(at) ? age(at) : '',
    href: waitingHref(it),
  }
}

function fromBoard(it: BoardItem, glyph: string, tone: GlyphTone, now: number): PopItem {
  const at = now - Date.parse(it.at)
  return {
    key: `lane:${it.key}`,
    glyph,
    tone,
    title: cutWord(it.name),
    full: it.name,
    where: it.detail,
    age: Number.isFinite(at) ? age(at) : '',
    href: it.laneId ? href.lane(it.laneId) : href.board(),
  }
}

/** The lists behind the four counts, built from the same snapshot and rules as the counts. */
export function popLists(snap: StatusInput, now: number): Record<CountKind, PopItem[]> {
  const items = boardItems(snap)
  const lane = (it: BoardItem) => (it.laneId ? `id:${it.laneId}` : `name:${it.repo}:${it.name}`)
  const broken = items.filter((it) => it.group === 'broken').map((it) => fromBoard(it, '✗', 'bad', now))
  const seen = new Set(items.filter((it) => it.group === 'broken').map(lane))
  for (const it of attentionOf(snap.waiting, 'blocked')) {
    const key = it.lane_id ? `id:${it.lane_id}` : `name:${it.repo ?? ''}:${it.lane ?? it.title}`
    if (!seen.has(key)) {
      seen.add(key)
      broken.push(fromWaiting(it, now))
    }
  }
  return {
    running: items.filter((it) => it.group === 'working').map((it) => fromBoard(it, '●', 'live', now)),
    waiting: waitingOnYou(snap.waiting).map((it) => fromWaiting(it, now)),
    broken,
    routing: attentionOf(snap.waiting, 'request').map((it) => fromWaiting(it, now)),
  }
}
