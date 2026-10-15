import { useCallback, useEffect, useState } from 'react'
import { topics as topicsApi } from '../api/client'
import { since } from '../format'
import { kindName, readFlag, splitTopics, topicHueClass, topicState, writeFlag, type TopicList, type TopicUpdateBody, type TopicView } from '../model/topic'
import { href } from '../router'
import { topicWaits } from '../state/attention'
import { useAttention } from '../state/context'
import { ErrorBoundary } from './ErrorBoundary'
import '../styles/topics.css'

const POLL = 15_000
export const IDLE_KEY = 'shepherd.topics.idle.open'

export type LoadTopics = (all: boolean) => Promise<TopicList>
export type ChangeTopic = (id: number, body: TopicUpdateBody) => Promise<unknown>
export type AddTopic = (title: string) => Promise<unknown>

/** Where a linked thing lives today, for the expanded row. */
function linkTarget(kind: string, ref: string): string | null {
  const n = Number(ref)
  if (!Number.isInteger(n) || n <= 0) return null
  if (kind === 'lane') return href.lane(n)
  if (kind === 'decision') return href.decisions(n)
  if (kind === 'request') return href.decisions()
  if (kind === 'next_item') return href.next()
  if (kind === 'run') return href.log(n)
  return null
}

/**
 * The topics on the chat rail: what the thread is about, most attention first. A row
 * filters the thread; clear folds a settled matter out of it and reopen brings it back.
 * Nothing here closes a lane or answers a decision.
 */
export function Topics(props: TopicsProps) {
  return (
    <ErrorBoundary region="The topics">
      <TopicsView {...props} />
    </ErrorBoundary>
  )
}

interface TopicsProps {
  /** The topic the thread is filtered to, or null. */
  active: number | null
  now: number
  onFilter: (id: number | null) => void
  load?: LoadTopics
  change?: ChangeTopic
  add?: AddTopic
}

function TopicsView({
  active, now, onFilter, load = (all) => topicsApi.list(all), change = topicsApi.update, add = topicsApi.add,
}: TopicsProps) {
  const attention = useAttention()
  const [list, setList] = useState<TopicView[] | null>(null)
  const [error, setError] = useState('')
  const [tick, setTick] = useState(0)
  const [open, setOpen] = useState<number | null>(null)
  const [showCleared, setShowCleared] = useState(false)
  const [showIdle, setShowIdle] = useState(() => readFlag(IDLE_KEY))
  const [title, setTitle] = useState('')
  const reload = useCallback(() => setTick((t) => t + 1), [])

  useEffect(() => {
    let live = true
    const read = () =>
      load(true).then(
        (l) => {
          if (!live) return
          setList(l.topics)
          setError('')
        },
        (e: unknown) => live && setError(e instanceof Error ? e.message : String(e)),
      )
    void read()
    const id = window.setInterval(() => void read(), POLL)
    return () => {
      live = false
      window.clearInterval(id)
    }
  }, [load, tick])

  const act = (p: Promise<unknown>) =>
    p.then(
      () => {
        setError('')
        reload()
      },
      (e: unknown) => setError(e instanceof Error ? e.message : String(e)),
    )

  const opened = (list ?? []).filter((t) => t.status === 'open')
  const cleared = (list ?? []).filter((t) => t.status === 'cleared')
  const { active: live, idle } = splitTopics(opened, now, active)
  const waits = (t: TopicView) => topicWaits(t, attention)
  const waiting = opened.reduce((n, t) => n + waits(t), 0)

  const row = (t: TopicView) => (
    <TopicRow
      key={t.id} t={t} waits={waits(t)} now={now} active={active === t.id} expanded={open === t.id} others={opened.filter((o) => o.id !== t.id)}
      onFilter={() => onFilter(active === t.id ? null : t.id)}
      onExpand={() => setOpen(open === t.id ? null : t.id)}
      change={(body) => act(change(t.id, body))}
    />
  )

  return (
    <section className="topics" aria-label="Topics">
      <h2 className="topics-head">
        <span className="topics-title">Topics</span>{' '}
        <span className="topics-counts">
          {list === null ? '' : `${opened.length} open${waiting > 0 ? ` · ${waiting} waiting` : ''}`}
        </span>
      </h2>
      {error && <p className="topics-error tone-bad">{error}</p>}
      {list !== null && opened.length === 0 && (
        <p className="faint">No topics yet. The front desk names the matter of a conversation as it goes.</p>
      )}
      {live.length > 0 && <ul className="topics-rows">{live.map(row)}</ul>}
      {idle.length > 0 && (
        <div className="topics-idle">
          <button
            type="button" className="link-button" aria-expanded={showIdle}
            onClick={() => { writeFlag(IDLE_KEY, !showIdle); setShowIdle(!showIdle) }}
          >
            Idle ({idle.length})
          </button>
          {showIdle && <ul className="topics-rows">{idle.map(row)}</ul>}
        </div>
      )}
      <form
        className="topics-new"
        onSubmit={(e) => {
          e.preventDefault()
          const v = title.trim()
          if (!v) return
          setTitle('')
          void act(add(v))
        }}
      >
        <label className="visually-hidden" htmlFor="topic-new">New topic</label>
        <input id="topic-new" value={title} maxLength={80} placeholder="New topic" onChange={(e) => setTitle(e.target.value)} />
        <button type="submit" className="button" disabled={title.trim() === ''}>Add</button>
      </form>
      {cleared.length > 0 && (
        <div className="topics-cleared">
          <button type="button" className="link-button" aria-expanded={showCleared} onClick={() => setShowCleared(!showCleared)}>
            Cleared ({cleared.length})
          </button>
          {showCleared && (
            <ul className="topics-rows">
              {cleared.map((t) => (
                <li key={t.id} className="topic-row is-cleared">
                  <span className="topic-title">{t.title}</span>
                  {t.reopen && <span className="topic-note tone-warn">reopen? something needs you</span>}
                  <button type="button" className="link-button" onClick={() => void act(change(t.id, { status: 'open' }))}>
                    Reopen
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </section>
  )
}

function TopicRow({
  t, waits, now, active, expanded, others, onFilter, onExpand, change,
}: {
  t: TopicView
  waits: number
  now: number
  active: boolean
  expanded: boolean
  others: TopicView[]
  onFilter: () => void
  onExpand: () => void
  change: (body: TopicUpdateBody) => void
}) {
  const [renaming, setRenaming] = useState(false)
  const [name, setName] = useState(t.title)
  const state = topicState(t)
  return (
    <li className={`topic-row ${topicHueClass(t.id)}${active ? ' is-active' : ''}${waits > 0 ? ' needs-you' : ''}`}>
      {renaming ? (
        <form
          className="topic-rename"
          onSubmit={(e) => {
            e.preventDefault()
            if (name.trim() && name.trim() !== t.title) change({ title: name.trim() })
            setRenaming(false)
          }}
        >
          <label className="visually-hidden" htmlFor={`topic-name-${t.id}`}>Topic title</label>
          <input id={`topic-name-${t.id}`} value={name} maxLength={80} onChange={(e) => setName(e.target.value)} />
          <button type="submit" className="button">Save</button>
        </form>
      ) : (
        <button type="button" className="topic-main" aria-pressed={active} onClick={onFilter}>
          <span className="topic-title">{t.title}</span>
          <span className="topic-meta faint">
            {state && <span className={waits > 0 ? 'tone-warn' : ''}>{state} · </span>}
            {since(t.activity, now) ? `${since(t.activity, now)} ago` : 'just now'}
          </span>
        </button>
      )}
      <div className="topic-actions">
        <button type="button" className="link-button" aria-expanded={expanded} onClick={onExpand}>
          {expanded ? 'Hide' : `Details (${t.links.length})`}
        </button>
        {!renaming && (
          <button type="button" className="link-button" onClick={() => { setName(t.title); setRenaming(true) }}>Rename</button>
        )}
        <button type="button" className="link-button" onClick={() => change({ status: 'cleared' })}>
          {t.settled ? 'Clear (settled)' : 'Clear'}
        </button>
      </div>
      {expanded && (
        <div className="topic-detail">
          {t.summary && <p>{t.summary}</p>}
          {t.links.length === 0 ? (
            <p className="faint">Nothing is tied to this topic yet.</p>
          ) : (
            <ul>
              {t.links.map((l) => {
                const to = linkTarget(l.kind, l.ref)
                const text = `${kindName(l.kind)} ${l.ref}${l.label ? `: ${l.label}` : ''}${l.state ? ` (${l.state})` : ''}`
                return (
                  <li key={`${l.kind}:${l.ref}`} className={l.waits ? 'tone-warn' : ''}>
                    {to ? <a href={to}>{text}</a> : text}
                    {l.waits ? ' · waits on you' : ''}
                  </li>
                )
              })}
            </ul>
          )}
          {others.length > 0 && (
            <label className="topic-merge">
              <span className="faint">Merge into</span>
              <select
                value=""
                onChange={(e) => {
                  const into = Number(e.target.value)
                  if (into > 0) change({ merge_into: into })
                }}
              >
                <option value="">choose…</option>
                {others.map((o) => (
                  <option key={o.id} value={o.id}>{o.title}</option>
                ))}
              </select>
            </label>
          )}
        </div>
      )}
    </li>
  )
}
