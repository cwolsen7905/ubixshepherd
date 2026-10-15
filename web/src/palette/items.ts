import { href } from '../router'
import type { Snapshot } from '../state/live'
import type { TopicView } from '../model/topic'

export type Group = 'Actions' | 'Pages' | 'Lanes' | 'Runs' | 'Decisions' | 'Topics'

/** Where choosing an item goes: a hash, or the one action that is not a page. */
export type Target = { to: string } | { act: 'new-conversation' }

export interface Item {
  id: string
  group: Group
  label: string
  /** A short second line: the repo, the state. */
  hint?: string
  /** The number it answers to, as typed ("12" or "#12"). */
  num?: number
  /** Words that match but are not shown. */
  keywords?: string
  target: Target
}

const PAGES: [string, string, string][] = [
  ['Decisions', href.decisions(), 'inbox waiting'],
  ['Chat', href.chat(), 'conversation desk'],
  ['Board', href.board(), 'lanes'],
  ['Next up', href.next(), 'plan'],
  ['Runs', href.runs(), 'agents'],
  ['Timeline', href.timeline(), 'history'],
  ['Projects', href.projects(), 'repos'],
  ['Usage', href.usage(), 'spend cost'],
  ['Settings', href.settings(), 'config'],
]

/** Where "answer next" goes: the oldest waiting decision, else the decisions page. */
export function answerNextTarget(snap: Pick<Snapshot, 'waiting' | 'decisions'>): string {
  const w = snap.waiting?.items.find((it) => it.kind === 'decision' && it.decision_id)
  if (w?.decision_id) return href.decisions(w.decision_id)
  const open = snap.decisions.find((d) => d.state === 'open')
  return open ? href.decisions(open.id) : href.decisions()
}

/** Everything the palette can reach, from what the app already holds. */
export function buildItems(
  snap: Pick<Snapshot, 'lanes' | 'runs' | 'decisions' | 'waiting'>,
  topics: Pick<TopicView, 'id' | 'title' | 'status'>[] = [],
): Item[] {
  const items: Item[] = [
    {
      id: 'act:answer-next',
      group: 'Actions',
      label: 'Answer next',
      hint: 'the oldest waiting decision',
      keywords: 'decision reply',
      target: { to: answerNextTarget(snap) },
    },
    {
      id: 'act:new-conversation',
      group: 'Actions',
      label: 'New conversation',
      hint: 'start the desk afresh',
      keywords: 'chat reset',
      target: { act: 'new-conversation' },
    },
  ]
  for (const [label, to, keywords] of PAGES) {
    items.push({
      id: `page:${label}`,
      group: 'Pages',
      label: `Go to ${label.toLowerCase()}`,
      keywords: `${label} ${keywords}`,
      target: { to },
    })
  }
  for (const l of snap.lanes) {
    if (l.state === 'closed') continue
    items.push({
      id: `lane:${l.id}`,
      group: 'Lanes',
      label: l.branch || l.name,
      hint: `lane ${l.id} · ${l.repo}`,
      num: l.id,
      keywords: `lane ${l.name} ${l.repo}`,
      target: { to: href.lane(l.id) },
    })
  }
  for (const r of snap.runs) {
    items.push({
      id: `run:${r.id}`,
      group: 'Runs',
      label: `Run ${r.id} in ${r.lane}`,
      hint: `${r.agent} · ${r.state}`,
      num: r.id,
      keywords: `run log ${r.agent} ${r.state} ${r.repo}`,
      target: { to: href.log(r.id) },
    })
  }
  for (const d of snap.decisions) {
    if (d.state !== 'open') continue
    items.push({
      id: `decision:${d.id}`,
      group: 'Decisions',
      label: d.question,
      hint: `decision ${d.id} · ${d.lane}`,
      num: d.id,
      keywords: `decision ${d.lane} ${d.repo}`,
      target: { to: href.decisions(d.id) },
    })
  }
  for (const t of topics) {
    if (t.status !== 'open') continue
    items.push({
      id: `topic:${t.id}`,
      group: 'Topics',
      label: t.title,
      hint: `topic ${t.id}`,
      num: t.id,
      keywords: 'topic',
      target: { to: href.chat() },
    })
  }
  return items
}

/**
 * Items for a query, best first. Every word must match the label, hint or keywords. A bare
 * number (or `#12`) matches an item's number, or `lane 12`, `run 12`, `decision 12`.
 */
export function match(items: Item[], query: string, limit = 40): Item[] {
  const q = query.trim().toLowerCase()
  if (!q) return items.slice(0, limit)
  const words = q
    .split(/\s+/)
    .map((w) => w.replace(/^#/, ''))
    .filter(Boolean)
  const scored: { it: Item; score: number; order: number }[] = []
  items.forEach((it, order) => {
    let total = 0
    for (const w of words) {
      const s = scoreWord(it, w)
      if (s < 0) return
      total += s
    }
    scored.push({ it, score: total, order })
  })
  scored.sort((a, b) => b.score - a.score || a.order - b.order)
  return scored.slice(0, limit).map((s) => s.it)
}

function scoreWord(it: Item, w: string): number {
  if (/^\d+$/.test(w) && it.num !== undefined) {
    if (String(it.num) === w) return 100
    return -1
  }
  const label = it.label.toLowerCase()
  if (label.startsWith(w)) return 60
  if (label.split(/[^a-z0-9]+/).some((p) => p.startsWith(w))) return 40
  if (label.includes(w)) return 25
  const rest = `${it.hint ?? ''} ${it.keywords ?? ''}`.toLowerCase()
  if (rest.split(/[^a-z0-9]+/).some((p) => p.startsWith(w))) return 15
  if (rest.includes(w)) return 8
  return -1
}
