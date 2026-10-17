import { useEffect, useMemo, useRef, useState } from 'react'
import { projects as projectsApi } from '../api/client'
import type { LaneView, ProjectSummary } from '../api/types'
import { clockTime } from '../format'
import { useKeys, useNow } from '../hooks'
import { markFor, type EventKind } from '../model/marks'
import type { FeedEntry } from '../model/feed'
import { go, href } from '../router'
import { useSnapshot } from '../state/context'
import { Chip, EmptyState, GroupHeader } from '../ui'
import { StatusGlyph, glyphTone } from '../ui/StatusGlyph'
import './History.css'

/** The kinds of the filter chips, each a set of the feed's events. The feed has no revert event yet, so a revert has no chip of its own. */
export const HISTORY_KINDS: { id: string; label: string; events: readonly EventKind[] }[] = [
  { id: 'run', label: 'Runs', events: ['run_started', 'run_ended', 'run_passed', 'run_failed', 'run_interrupted', 'run_quota', 'commit', 'report', 'gate'] },
  { id: 'answer', label: 'Answers', events: ['decision_asked', 'decision_answered', 'request', 'request_attention'] },
  { id: 'merge', label: 'Merges', events: ['mr', 'mr_approve', 'pipeline', 'tag', 'release'] },
  { id: 'lane', label: 'Lanes', events: ['lane_opened', 'lane_closed'] },
  { id: 'config', label: 'Config', events: ['config', 'budget', 'plan', 'desk_rotated', 'info'] },
]

/** The date ranges of the range select; `days` 0 is since midnight, null is everything the feed holds. */
const RANGES: { id: string; label: string; phrase: string; days: number | null }[] = [
  { id: 'today', label: 'Today', phrase: 'today', days: 0 },
  { id: '7d', label: 'Last 7 days', phrase: 'in the last 7 days', days: 7 },
  { id: '30d', label: 'Last 30 days', phrase: 'in the last 30 days', days: 30 },
  { id: 'all', label: 'All time', phrase: 'in the feed', days: null },
]

function startOfDay(ms: number): number {
  const d = new Date(ms)
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
}

/** The events whose ref is a lane id. */
const LANE_EVENTS: readonly EventKind[] = ['lane_opened', 'lane_closed', 'gate', 'mr', 'mr_approve', 'pipeline']

/** The page an entry is about, when its ref names one. */
export function entryHref(e: FeedEntry): string | undefined {
  if (!e.ref) return undefined
  if (e.event.startsWith('run_') || e.event === 'commit' || e.event === 'report') return href.log(e.ref)
  if (e.event === 'lane_opened' || e.event === 'lane_closed' || e.event === 'gate' || e.event === 'mr' || e.event === 'mr_approve' || e.event === 'pipeline') {
    return href.lane(e.ref)
  }
  return undefined
}

/** Whether `text` names `name` as a whole word, so a repo `api` does not match `api-gateway` or `rapid`. */
export function mentions(text: string, name: string): boolean {
  const esc = name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return new RegExp(`(^|[^\\w/.-])${esc}($|[^\\w.-])`).test(text)
}

function dayKey(iso: string): string {
  const d = new Date(iso)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function dayLabel(key: string, now: number): string {
  if (key === dayKey(new Date(now).toISOString())) return 'Today'
  if (key === dayKey(new Date(now - 24 * 3600 * 1000).toISOString())) return 'Yesterday'
  const [y, m, d] = key.split('-').map(Number) as [number, number, number]
  return new Date(y, m - 1, d).toLocaleDateString('en-US', { weekday: 'short', month: 'short', day: 'numeric' })
}

/** A searchable log of what happened, newest first, grouped by day, from the live feed. */
export function HistoryPage({ kind = '', project = '', q = '' }: { kind?: string; project?: string; q?: string }) {
  const snap = useSnapshot()
  const now = useNow(60_000)
  const [list, setList] = useState<ProjectSummary[]>([])
  const [listed, setListed] = useState(false)
  const [text, setText] = useState(q)
  const [range, setRange] = useState('7d')
  const root = useRef<HTMLDivElement>(null)
  useEffect(() => {
    let live = true
    projectsApi.list().then(
      (p) => {
        if (!live) return
        setList(p.projects)
        setListed(true)
      },
      () => live && setListed(true),
    )
    return () => {
      live = false
    }
  }, [])

  const set = (o: { kind?: string; project?: string; q?: string }) => go(href.history({ kind, project, q, ...o }))

  const names = useMemo(() => {
    const repos = new Set(list.find((p) => p.name === project)?.repos ?? [])
    if (!project) return null
    const out = new Set<string>(repos)
    for (const l of snap.lanes) if (repos.has(l.repo)) out.add(l.name)
    return [...out]
  }, [list, project, snap.lanes])

  const span = RANGES.find((r) => r.id === range) ?? RANGES[1] as (typeof RANGES)[number]
  const inRange = useMemo(() => {
    const from = span.days === null ? 0 : span.days === 0 ? startOfDay(now) : now - span.days * 24 * 3600 * 1000
    return [...snap.feed].reverse().filter((e) => new Date(e.created).getTime() >= from)
  }, [snap.feed, span, now])

  const needle = q.trim().toLowerCase()
  const base = useMemo(
    () =>
      inRange
        .filter((e) => !names || names.some((n) => mentions(e.text, n)))
        .filter((e) => !needle || `${e.text} ${markFor(e.event).label}`.toLowerCase().includes(needle)),
    [inRange, names, needle],
  )
  const events = HISTORY_KINDS.find((k) => k.id === kind)?.events
  const shown = useMemo(() => (events ? base.filter((e) => events.includes(e.event)) : base), [base, events])
  const counts = useMemo(() => Object.fromEntries(HISTORY_KINDS.map((k) => [k.id, base.filter((e) => k.events.includes(e.event)).length])), [base])
  const days = useMemo(() => {
    const out: { key: string; items: FeedEntry[] }[] = []
    for (const e of shown) {
      const key = dayKey(e.created)
      const last = out.at(-1)
      if (last?.key === key) last.items.push(e)
      else out.push({ key, items: [e] })
    }
    return out
  }, [shown])

  /**
   * The lane an entry is about, for the link at the right of its row, built from ids only, never from the text.
   * A lane event's ref is the lane id; a run event's ref is a run, whose lane id comes from the snapshot's runs.
   * An entry with no such id, or whose lane or run is not in the snapshot, shows no link.
   */
  const laneOf = (e: FeedEntry): LaneView | undefined => {
    if (!e.ref) return undefined
    if (e.event.startsWith('run_') || e.event === 'commit' || e.event === 'report') {
      const run = snap.runs.find((r) => r.id === e.ref)
      return run && snap.lanes.find((l) => l.id === run.lane_id)
    }
    if (LANE_EVENTS.includes(e.event)) return snap.lanes.find((l) => l.id === e.ref)
    return undefined
  }

  const move = (by: number) => {
    const els = [...(root.current?.querySelectorAll<HTMLElement>('.history-list .sh-row') ?? [])]
    const at = els.indexOf(document.activeElement as HTMLElement)
    els[Math.min(Math.max(at + by, 0), els.length - 1)]?.focus()
  }
  useKeys({ j: () => move(1), k: () => move(-1) }, [shown])

  return (
    <div ref={root} className="page history-page" data-component="History">
      <header className="page-head">
        <h1>History</h1>
        <p className="history-count faint">
          {shown.length} of {inRange.length} {span.phrase}
        </p>
      </header>
      <form
        className="history-search"
        role="search"
        onSubmit={(e) => {
          e.preventDefault()
          set({ q: text })
        }}
      >
        <input type="search" className="sh-input history-q" aria-label="Search history" placeholder="Search runs, answers, merges" value={text} onChange={(e) => setText(e.target.value)} />
        <div className="history-kinds" role="group" aria-label="Kind">
          <Chip kind="filter" selected={!kind} onClick={() => set({ kind: '' })}>
            All <span className="history-n">{base.length}</span>
          </Chip>
          {HISTORY_KINDS.map((k) => (
            <Chip key={k.id} kind="filter" selected={kind === k.id} onClick={() => set({ kind: kind === k.id ? '' : k.id })}>
              {k.label} <span className="history-n">{counts[k.id]}</span>
            </Chip>
          ))}
        </div>
        {list.length > 1 && (
          <select className="sh-input history-select" aria-label="Project" value={project} onChange={(e) => set({ project: e.target.value })}>
            <option value="">All projects</option>
            {list.map((p) => (
              <option key={p.name} value={p.name}>{p.name}</option>
            ))}
          </select>
        )}
        <select className="sh-input history-select" aria-label="Date range" value={range} onChange={(e) => setRange(e.target.value)}>
          {RANGES.map((r) => (
            <option key={r.id} value={r.id}>{r.label}</option>
          ))}
        </select>
      </form>
      {days.length === 0 ? (
        <EmptyState title={snap.ready && (listed || !project) ? 'Nothing matches.' : 'Loading…'}>
          {snap.ready && (listed || !project) && (snap.feed.length === 0 ? 'Nothing has happened in the feed yet.' : inRange.length === 0 ? 'Nothing in this range: choose a longer one.' : 'Clear a filter to see more.')}
        </EmptyState>
      ) : (
        days.map((d) => (
          <section key={d.key} aria-label={dayLabel(d.key, now)}>
            <GroupHeader label={dayLabel(d.key, now)} />
            <ul className="history-list">
              {d.items.map((e) => {
                const m = markFor(e.event)
                const to = entryHref(e)
                const lane = laneOf(e)
                return (
                  <li key={e.id}>
                    <div
                      className="sh-row sh-row--time history-row"
                      tabIndex={0}
                      role={to ? 'link' : undefined}
                      data-component="HistoryRow"
                      onClick={(ev) => to && !(ev.target as HTMLElement).closest('a') && go(to)}
                      onKeyDown={(ev) => {
                        if (to && ev.key === 'Enter' && ev.target === ev.currentTarget) go(to)
                      }}
                    >
                      <time className="history-time" dateTime={e.created}>{clockTime(e.created, now).slice(-5)}</time>
                      <StatusGlyph glyph={m.glyph} tone={glyphTone(m.tone)} label={m.label} />
                      <span className="sh-row-main">
                        {to ? <a className="sh-row-title" href={to} tabIndex={-1}>{e.text}</a> : <span className="sh-row-title">{e.text}</span>}
                        <span className="sh-row-sub">{m.label}</span>
                      </span>
                      {lane && <a className="history-lane" href={href.lane(lane.id)}>{lane.name}</a>}
                    </div>
                  </li>
                )
              })}
            </ul>
          </section>
        ))
      )}
    </div>
  )
}
