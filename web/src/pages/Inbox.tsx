import { useEffect, useMemo, useState } from 'react'
import { api, ApiError, waitingOnYou } from '../api/client'
import type { DecisionView, WaitingItem } from '../api/types'
import { splitQuestion } from '../components/DecisionCard'
import { Glyph } from '../components/Glyph'
import { inline } from '../conversation/Markdown'
import { since } from '../format'
import { useKeys, useNow } from '../hooks'
import { neighbour, openKey, waitingKey } from '../model/waitingStep'
import { go, href, parse, useFocusTarget, withOpen, type Open } from '../router'
import { useLive, useSnapshot } from '../state/context'
import '../styles/pages/inbox.css'

type Group = 'answer' | 'approve' | 'broken'

const GROUPS: { key: Group; title: string; kinds: WaitingItem['kind'][] }[] = [
  { key: 'answer', title: 'Answer', kinds: ['decision', 'question', 'request'] },
  { key: 'approve', title: 'Approve', kinds: ['mr', 'next', 'ship', 'scope'] },
  { key: 'broken', title: 'Broken', kinds: ['blocked'] },
]

const KIND_LABEL: Record<WaitingItem['kind'], string> = {
  decision: 'decision',
  question: 'question',
  request: 'request to route',
  mr: 'merge request',
  next: 'next up',
  ship: 'to ship',
  scope: 'scope request',
  blocked: 'blocked',
}

const KIND_FILTERS = ['decision', 'question', 'request', 'mr', 'next', 'ship', 'scope', 'blocked'] as const

/** What opening a row shows in the inspector: the item's own pane, else its lane. */
export function inboxOpen(it: WaitingItem): Open | null {
  if (it.kind === 'decision' && it.decision_id) return { kind: 'decision', id: it.decision_id }
  if (it.kind === 'question' && it.question_id) return { kind: 'question', id: it.question_id }
  if (it.lane_id) return { kind: 'lane', id: it.lane_id }
  return null
}

/** The page that holds an item the inspector has no pane for. */
function pageFor(it: WaitingItem): string | null {
  if (it.kind === 'next' && it.next_id) return href.next(it.next_id)
  return null
}

function show(it: WaitingItem) {
  const o = inboxOpen(it)
  if (o) {
    const now = parse(window.location.hash).open
    // The row already open fires no hashchange: bring the inspector's heading into focus instead.
    if (now && now.kind === o.kind && now.id === o.id) document.getElementById('inspector-title')?.focus()
    else go(withOpen(window.location.hash, o))
  } else {
    const to = pageFor(it)
    if (to) go(to)
  }
}

/** Is `it` the item the inspector holds? Decisions and questions match by id, the rest by lane. */
function isOpen(it: WaitingItem, open: Open | undefined): boolean {
  const o = inboxOpen(it)
  return !!open && !!o && o.kind === open.kind && o.id === open.id
}

/**
 * Everything that waits on the person in one list, grouped by what they do. `j` and `k` move,
 * `Enter` opens the row in the inspector, and answering there advances to the next row.
 */
export function InboxPage({ kind }: { kind?: string }) {
  const snap = useSnapshot()
  const live = useLive()
  const now = useNow()
  const all = useMemo(() => snap.waiting?.items ?? [], [snap.waiting?.items])
  const items = useMemo(() => (kind ? all.filter((it) => it.kind === kind) : all), [all, kind])
  const [cursor, setCursor] = useState<string | null>(null)
  const [showDone, setShowDone] = useState(false)
  const [done, setDone] = useState<DecisionView[] | null>(null)
  const open = parse(window.location.hash).open
  const [notes, setNotes] = useState<Record<string, { text: string; bad: boolean }>>({})
  const [shipping, setShipping] = useState<string | null>(null)
  // A deep link (including the old #/decisions?d=N) scrolls its decision to the middle and marks it.
  const openDecision = open?.kind === 'decision' ? open.id : undefined
  const lit = useFocusTarget(openDecision, all.some((it) => it.decision_id === openDecision), (id) => `[data-decision-id="${id}"]`)

  const ship = async (it: WaitingItem) => {
    const key = waitingKey(it)
    if (!it.lane_id) return
    setShipping(key)
    try {
      const r = await waitingOnYou.ship(it.lane_id)
      setNotes((n) => ({ ...n, [key]: { text: r.message || 'Shipped.', bad: false } }))
    } catch (e) {
      const text = e instanceof ApiError || e instanceof Error ? e.message : String(e)
      setNotes((n) => ({ ...n, [key]: { text: `Not shipped: ${text}`, bad: true } }))
    } finally {
      setShipping(null)
      void live.readWaiting()
    }
  }

  useEffect(() => {
    if (!showDone) return
    let live = true
    api.decisions('answered').then(
      (ds) => live && setDone(ds.slice(-30).reverse()),
      () => live && setDone([]),
    )
    return () => {
      live = false
    }
  }, [showDone, snap.decisions.length])

  const at = cursor ?? openKey(items, open) ?? null
  const move = (dir: 'up' | 'down') => {
    const it = neighbour(items, at, dir)
    if (it) setCursor(waitingKey(it))
  }
  useKeys(
    {
      j: () => move('down'),
      k: () => move('up'),
      Enter: (e) => {
        const it = items.find((x) => waitingKey(x) === at)
        if (!it || (e.target instanceof HTMLElement && e.target.closest('button, a'))) return
        show(it)
      },
    },
    [items, at],
  )

  const loaded = snap.waiting !== null
  return (
    <div className="page inbox">
      <header className="page-head">
        <h1>Inbox</h1>
        <p className="page-sub">
          {!loaded
            ? 'Loading…'
            : all.length === 0
              ? 'Nothing waits on you.'
              : `${all.length} waiting on you, oldest first. Only you answer decisions and approve work.`}
        </p>
      </header>

      <nav className="inbox-filter" aria-label="Filter by kind">
        <a href={href.inbox()} aria-current={!kind ? 'page' : undefined}>All</a>
        {KIND_FILTERS.map((k) => {
          const n = all.filter((it) => it.kind === k).length
          if (n === 0 && kind !== k) return null
          return (
            <a key={k} href={href.inbox(k)} aria-current={kind === k ? 'page' : undefined}>
              {KIND_LABEL[k]} {n}
            </a>
          )
        })}
      </nav>

      {loaded && items.length === 0 && all.length > 0 && <p className="faint">Nothing of this kind waits on you.</p>}

      {GROUPS.map((g) => {
        const rows = items.filter((it) => g.kinds.includes(it.kind))
        if (rows.length === 0) return null
        return (
          <section key={g.key} className="inbox-group" aria-labelledby={`inbox-${g.key}`}>
            <h2 id={`inbox-${g.key}`} className="section-h">
              {g.title} <span className="faint">{rows.length}</span>
            </h2>
            <ul className="inbox-list">
              {rows.map((it) => {
                const key = waitingKey(it) + (it.request_id ? `:r${it.request_id}` : '')
                const where = it.lane ? (it.repo ? `${it.repo}:${it.lane}` : it.lane) : it.repo
                const selected = isOpen(it, open)
                const actionable = inboxOpen(it) !== null || pageFor(it) !== null
                if (!actionable) {
                  // Nothing to open: say what the person does instead.
                  return (
                    <li key={key} className="inbox-row">
                      <div className="inbox-open">
                        <span className="inbox-kind faint">{KIND_LABEL[it.kind]}</span>
                        <span className="inbox-title">{inline(splitQuestion(it.title).head || it.title)}</span>
                        <span className="inbox-meta faint">
                          {where}
                          {it.since ? ` · waiting ${since(it.since, now)}` : ''}
                          {it.kind === 'request' && it.request_id ? (
                            <>
                              {' · route it with '}
                              <code>shepherd request route {it.request_id} --lane &lt;lane&gt;</code>
                            </>
                          ) : null}
                        </span>
                      </div>
                    </li>
                  )
                }
                return (
                  <li
                    key={key}
                    className={`inbox-row${waitingKey(it) === at ? ' is-cursor' : ''}${it.decision_id && lit === it.decision_id ? ' is-focus-target' : ''}`}
                  >
                    <button
                      type="button"
                      className="inbox-open"
                      data-waiting-key={waitingKey(it)}
                      data-decision-id={it.decision_id}
                      aria-current={selected ? 'true' : undefined}
                      onClick={() => {
                        setCursor(waitingKey(it))
                        show(it)
                      }}
                    >
                      <span className="inbox-kind faint">{KIND_LABEL[it.kind]}</span>
                      <span className="inbox-title">{inline(splitQuestion(it.title).head || it.title)}</span>
                      <span className="inbox-meta faint">
                        {where}
                        {it.since ? ` · waiting ${since(it.since, now)}` : ''}
                      </span>
                    </button>
                    {it.action === 'ship' && it.lane_id ? (
                      <button type="button" className="inbox-act" disabled={shipping === waitingKey(it)} onClick={() => void ship(it)}>
                        {shipping === waitingKey(it) ? 'Shipping…' : 'Ship'}
                      </button>
                    ) : null}
                    {notes[waitingKey(it)] && (
                      <p className={`inbox-note${notes[waitingKey(it)]?.bad ? ' tone-bad' : ''}`} role={notes[waitingKey(it)]?.bad ? 'alert' : 'status'}>
                        {notes[waitingKey(it)]?.text}
                      </p>
                    )}
                  </li>
                )
              })}
            </ul>
          </section>
        )
      })}

      <div className="page-foot">
        <button type="button" className="link-button" onClick={() => setShowDone((s) => !s)} aria-expanded={showDone}>
          {showDone ? 'Hide answered decisions' : 'Show answered decisions'}
        </button>
      </div>
      {showDone && (
        <ol className="answered">
          {done === null && <li className="faint">Loading…</li>}
          {done?.length === 0 && <li className="faint">None answered yet.</li>}
          {done?.map((d) => (
            <li key={d.id}>
              <Glyph glyph="↳" tone="muted" label="answered" />
              <span>
                <span className="faint">decision {d.id} · {d.lane} · {d.agent}</span>
                <br />
                {inline(splitQuestion(d.question).head || d.question)}
                <br />
                <strong>{d.answer}</strong>
              </span>
            </li>
          ))}
        </ol>
      )}
    </div>
  )
}
