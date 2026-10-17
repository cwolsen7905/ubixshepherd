import { useEffect, useMemo, useRef, useState } from 'react'
import { projects as projectsApi } from '../api/client'
import type { ProjectSummary } from '../api/types'
import { clockTime } from '../format'
import { useKeys, useNow } from '../hooks'
import { markFor, type EventKind } from '../model/marks'
import type { FeedEntry } from '../model/feed'
import { go, href } from '../router'
import { useSnapshot } from '../state/context'
import { Chip, EmptyState, GroupHeader, ListRow } from '../ui'
import { glyphTone } from '../ui/StatusGlyph'
import './History.css'

/** The kinds of the filter chips, each a set of the feed's events. Reverts have no event of their own yet: a revert shows under merges when the feed names one. */
export const HISTORY_KINDS: { id: string; label: string; events: readonly EventKind[] }[] = [
  { id: 'run', label: 'Runs', events: ['run_started', 'run_ended', 'run_passed', 'run_failed', 'run_interrupted', 'run_quota', 'commit', 'report', 'gate'] },
  { id: 'answer', label: 'Answers', events: ['decision_asked', 'decision_answered', 'request', 'request_attention'] },
  { id: 'merge', label: 'Merges', events: ['mr', 'mr_approve', 'pipeline', 'tag', 'release'] },
  { id: 'lane', label: 'Lanes', events: ['lane_opened', 'lane_closed'] },
  { id: 'config', label: 'Config', events: ['config', 'budget', 'plan', 'desk_rotated', 'info'] },
]

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

  const events = HISTORY_KINDS.find((k) => k.id === kind)?.events
  const needle = q.trim().toLowerCase()
  const shown = useMemo(
    () =>
      [...snap.feed]
        .reverse()
        .filter((e) => (!events || events.includes(e.event)) && (!names || names.some((n) => mentions(e.text, n))))
        .filter((e) => !needle || `${e.text} ${markFor(e.event).label}`.toLowerCase().includes(needle)),
    [snap.feed, events, names, needle],
  )
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
        <p className="page-sub faint">What happened, newest first, from the daemon's feed.</p>
      </header>
      <form
        className="history-search"
        role="search"
        onSubmit={(e) => {
          e.preventDefault()
          set({ q: text })
        }}
      >
        <input type="search" aria-label="Search history" placeholder="Search runs, answers, merges" value={text} onChange={(e) => setText(e.target.value)} />
        {list.length > 1 && (
          <select aria-label="Project" value={project} onChange={(e) => set({ project: e.target.value })}>
            <option value="">All projects</option>
            {list.map((p) => (
              <option key={p.name} value={p.name}>{p.name}</option>
            ))}
          </select>
        )}
      </form>
      <div className="history-kinds" role="group" aria-label="Kind">
        <Chip kind="filter" selected={!kind} onClick={() => set({ kind: '' })}>All</Chip>
        {HISTORY_KINDS.map((k) => (
          <Chip key={k.id} kind="filter" selected={kind === k.id} onClick={() => set({ kind: kind === k.id ? '' : k.id })}>
            {k.label}
          </Chip>
        ))}
      </div>
      {days.length === 0 ? (
        <EmptyState title={snap.ready && (listed || !project) ? 'Nothing matches.' : 'Loading…'}>
          {snap.ready && (listed || !project) && (kind || project || needle ? 'Clear a filter to see more.' : 'Nothing has happened in the feed yet.')}
        </EmptyState>
      ) : (
        days.map((d) => (
          <section key={d.key} aria-label={dayLabel(d.key, now)}>
            <GroupHeader label={dayLabel(d.key, now)} hint={`${d.items.length}`} />
            <ul className="history-list">
              {d.items.map((e) => {
                const m = markFor(e.event)
                return (
                  <li key={e.id}>
                    <ListRow
                      tabIndex={0}
                      glyph={m.glyph}
                      tone={glyphTone(m.tone)}
                      glyphLabel={m.label}
                      title={e.text}
                      sub={m.label}
                      age={clockTime(e.created, now)}
                      href={entryHref(e)}
                    />
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
