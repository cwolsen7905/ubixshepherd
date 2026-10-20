import { useEffect, useMemo, useRef, useState } from 'react'
import { api, ApiError, history, nextUp, waitingOnYou } from '../api/client'
import type { LaneRecord, WaitingItem } from '../api/types'
import { splitQuestion } from '../components/DecisionCard'
import { inline } from '../conversation/Markdown'
import { since } from '../format'
import { useKeys, useNow } from '../hooks'
import { isSuggestion, neighbour, openKey, SUGGESTION_LABEL, suggestionChoices, suggestionPatch, waitingKey, type SuggestionChoice } from '../model/waitingStep'
import { go, href, parse, useFocusTarget, withOpen, type Open } from '../router'
import { useLive, useSnapshot } from '../state/context'
import { Button, Chip, Count, EmptyState, GroupHeader, Kbd, ListRow, LoadingRows } from '../ui'
import type { GlyphTone } from '../ui'
import './Inbox.css'

type Group = 'answer' | 'approve' | 'broken' | 'other'
type Show = 'waiting' | 'broken' | 'done'

const GROUPS: { key: Group; title: string; kinds: WaitingItem['kind'][] }[] = [
  { key: 'answer', title: 'Answer', kinds: ['decision', 'question', 'request'] },
  { key: 'approve', title: 'Approve', kinds: ['mr', 'next', 'ship', 'scope'] },
  { key: 'broken', title: 'Broken', kinds: ['blocked'] },
]

/** Kinds some group lists; housekeeping (a merged lane's undecided files) and any kind a newer daemon adds go under To tidy, so the rows match the count. */
const GROUPED = new Set<string>(GROUPS.flatMap((g) => g.kinds))
const OTHER = { key: 'other' as Group, title: 'To tidy', kinds: [] as WaitingItem['kind'][] }

const KIND_LABEL: Record<WaitingItem['kind'], string> = {
  decision: 'decision',
  question: 'question',
  request: 'request to route',
  mr: 'merge request',
  next: 'next up',
  ship: 'to ship',
  scope: 'scope request',
  blocked: 'blocked',
  artifact: 'files to decide',
}

/** Each kind's glyph and tone: `?` decision, `!` request, `◆` approve, `≡` next up, `✗` broken. */
const MARK: Record<WaitingItem['kind'], { glyph: string; tone: GlyphTone }> = {
  decision: { glyph: '?', tone: 'warn' },
  question: { glyph: '?', tone: 'warn' },
  request: { glyph: '!', tone: 'warn' },
  mr: { glyph: '◆', tone: 'warn' },
  next: { glyph: '≡', tone: 'warn' },
  ship: { glyph: '◆', tone: 'warn' },
  scope: { glyph: '!', tone: 'warn' },
  blocked: { glyph: '✗', tone: 'bad' },
  artifact: { glyph: '▤', tone: 'warn' },
}

/** A kind this page does not know yet (a newer daemon) still shows, with a neutral mark. */
const UNKNOWN_MARK: { glyph: string; tone: GlyphTone } = { glyph: '○', tone: 'idle' }

/** The least time between two reads of what finished today. */
const MIN_REREAD = 15_000

function startOfDay(now: number): Date {
  const d = new Date(now)
  d.setHours(0, 0, 0, 0)
  return d
}

/** What opening a row shows in the inspector: the item's own pane, else its lane. */
export function inboxOpen(it: WaitingItem): Open | null {
  if (it.kind === 'decision' && it.decision_id) return { kind: 'decision', id: it.decision_id }
  if (it.kind === 'question' && it.question_id) return { kind: 'question', id: it.question_id }
  if (it.kind === 'mr' && it.lane_id) return { kind: 'mr', id: it.lane_id }
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

/**
 * Everything that waits on the person in one list, grouped by what they do, then what finished
 * today. `j` and `k` move, `Enter` opens the row in the side panel, `]` jumps to the next one,
 * and answering or approving there removes the row and opens the next.
 */
export function InboxPage({ kind }: { kind?: string }) {
  const snap = useSnapshot()
  const live = useLive()
  const now = useNow()
  const all = useMemo(() => snap.waiting?.items ?? [], [snap.waiting?.items])
  const items = useMemo(() => (kind ? all.filter((it) => it.kind === kind) : all), [all, kind])
  const [cursor, setCursor] = useState<string | null>(null)
  const [only, setOnly] = useState<Show | null>(null)
  const [merged, setMerged] = useState<LaneRecord[]>([])
  const [answered, setAnswered] = useState(0)
  const open = parse(window.location.hash).open
  const [notes, setNotes] = useState<Record<string, { text: string; bad: boolean }>>({})
  const [shipping, setBusy] = useState<string | null>(null)
  // A deep link (including the old #/decisions?d=N) scrolls its decision to the middle and marks it.
  const openDecision = open?.kind === 'decision' ? open.id : undefined
  const lit = useFocusTarget(openDecision, all.some((it) => it.decision_id === openDecision), (id) => `[data-decision-id="${id}"]`)

  const ship = async (it: WaitingItem) => {
    const key = waitingKey(it)
    if (!it.lane_id) return
    setBusy(key)
    try {
      const r = await waitingOnYou.ship(it.lane_id)
      setNotes((n) => ({ ...n, [key]: { text: r.message || 'Shipped.', bad: false } }))
    } catch (e) {
      const text = e instanceof ApiError || e instanceof Error ? e.message : String(e)
      setNotes((n) => ({ ...n, [key]: { text: `Not shipped: ${text}`, bad: true } }))
    } finally {
      setBusy(null)
      void live.readWaiting()
    }
  }

  // Answering a "done?" suggestion is the person's click: done, drop, or still needed.
  const settle = async (it: WaitingItem, choice: SuggestionChoice) => {
    const key = waitingKey(it)
    if (!it.next_id) return
    setBusy(key)
    try {
      await nextUp.update(it.next_id, suggestionPatch(choice))
      setNotes((n) => Object.fromEntries(Object.entries(n).filter(([k]) => k !== key)))
    } catch (e) {
      const text = e instanceof ApiError || e instanceof Error ? e.message : String(e)
      setNotes((n) => ({ ...n, [key]: { text: `Not changed: ${text}`, bad: true } }))
    } finally {
      setBusy(null)
      void live.readWaiting()
    }
  }

  // Done today: merge requests that merged since local midnight, and decisions answered since.
  const workspaceIds = (snap.status?.workspaces ?? []).map((w) => w.id).join(',')
  const feedLast = snap.feed.at(-1)?.id ?? 0
  const lastRead = useRef(0)
  const readIds = useRef<string | null>(null)
  useEffect(() => {
    let current = true
    const read = () => {
      lastRead.current = Date.now()
      readIds.current = workspaceIds
      const from = startOfDay(Date.now())
      const ids = workspaceIds === '' ? [] : workspaceIds.split(',').map(Number)
      Promise.all(ids.map((id) => history.lanes(id, from.toISOString()))).then(
        (ls) => current && setMerged(ls.flat().filter((l) => l.outcome === 'merged' && l.closed && Date.parse(l.closed) >= from.getTime()).sort((a, b) => Date.parse(b.closed ?? '') - Date.parse(a.closed ?? ''))),
        () => current && setMerged([]),
      )
      api.decisions('answered').then(
        (ds) => current && setAnswered(ds.filter((d) => d.answered && Date.parse(d.answered) >= from.getTime()).length),
        () => current && setAnswered(0),
      )
    }
    // A busy feed reads at most once in MIN_REREAD, with one trailing read when the window ends.
    // A new workspace list reads at once: the first read ran before it arrived and had no repos to ask.
    const wait = readIds.current !== workspaceIds ? 0 : MIN_REREAD - (Date.now() - lastRead.current)
    if (wait <= 0) read()
    else {
      const t = window.setTimeout(read, wait)
      return () => {
        current = false
        window.clearTimeout(t)
      }
    }
    return () => {
      current = false
    }
  }, [workspaceIds, feedLast])

  // The row whose panel is open: the shared openKey knows decisions and questions only, so merge
  // requests (and lanes) match through inboxOpen. A panel that changes drops the cursor.
  const openRow = open ? items.find((it) => { const o = inboxOpen(it); return o !== null && o.kind === open.kind && o.id === open.id }) : undefined
  const openAt = (openRow ? waitingKey(openRow) : openKey(items, open)) ?? null
  const [seenOpen, setSeenOpen] = useState(openAt)
  if (seenOpen !== openAt) {
    setSeenOpen(openAt)
    setCursor(null)
  }
  const at = cursor ?? openAt
  const move = (dir: 'up' | 'down') => {
    const it = neighbour(items, at, dir)
    if (it) setCursor(waitingKey(it))
  }
  useKeys(
    {
      j: () => move('down'),
      k: () => move('up'),
      ']': () => {
        const it = neighbour(items, at, 'down')
        if (!it) return
        setCursor(waitingKey(it))
        show(it)
      },
      Enter: (e) => {
        const it = items.find((x) => waitingKey(x) === at)
        if (!it || (e.target instanceof HTMLElement && e.target.closest('button, a, [role="button"]'))) return
        show(it)
      },
    },
    [items, at],
  )

  const loaded = snap.waiting !== null
  const waitingN = items.filter((it) => it.kind !== 'blocked').length
  const brokenN = items.filter((it) => it.kind === 'blocked').length
  const doneN = merged.length + answered
  const shown = (g: Show) => only === null || only === g
  const chip = (g: Show, label: string, n: number, tone: 'attention' | 'bad' | 'neutral') => (
    <Chip kind="filter" selected={only === g} onClick={() => setOnly(only === g ? null : g)}>
      {label} <Count count={n} tone={tone} showZero label={label.toLowerCase()} />
    </Chip>
  )

  const row = (it: WaitingItem) => {
    const key = waitingKey(it) + (it.request_id ? `:r${it.request_id}` : '')
    const where = it.lane ? (it.repo ? `${it.repo}:${it.lane}` : it.lane) : it.repo
    const actionable = inboxOpen(it) !== null || pageFor(it) !== null
    const note = notes[waitingKey(it)]
    const mark = MARK[it.kind] ?? UNKNOWN_MARK
    const sub = (
      <>
        {[KIND_LABEL[it.kind] ?? it.kind, where].filter(Boolean).join(' · ')}
        {it.kind === 'request' && it.request_id && !actionable ? (
          <>
            {' · route it with '}
            <code>shepherd request route {it.request_id} --lane &lt;lane&gt;</code>
          </>
        ) : null}
        {isSuggestion(it) && it.detail ? ` · ${it.detail}` : ''}
      </>
    )
    const title = inline(splitQuestion(it.title).head || it.title)
    return (
      <li
        key={key}
        className={`inbox-item${it.decision_id && lit === it.decision_id ? ' is-focus-target' : ''}`}
        data-component="InboxItem"
        data-waiting-key={waitingKey(it)}
        data-decision-id={it.decision_id}
      >
        <ListRow
          glyph={mark.glyph}
          tone={mark.tone}
          glyphLabel={KIND_LABEL[it.kind] ?? it.kind}
          title={title}
          sub={sub}
          age={it.since ? since(it.since, now) : undefined}
          selected={waitingKey(it) === at}
          onSelect={actionable ? () => { setCursor(waitingKey(it)); show(it) } : undefined}
        />
        {(isSuggestion(it) || (it.action === 'ship' && it.lane_id)) && (
          <div className="inbox-actions">
            {isSuggestion(it)
              ? suggestionChoices(it).map((c) => (
                  <Button key={c} size="small" disabled={shipping === waitingKey(it)} onClick={() => void settle(it, c)}>
                    {SUGGESTION_LABEL[c]}
                  </Button>
                ))
              : null}
            {it.action === 'ship' && it.lane_id ? (
              <Button size="small" disabled={shipping === waitingKey(it)} onClick={() => void ship(it)}>
                {shipping === waitingKey(it) ? 'Shipping…' : 'Ship'}
              </Button>
            ) : null}
          </div>
        )}
        {note && (
          <p className={`inbox-note${note.bad ? ' tone-bad' : ''}`} role={note.bad ? 'alert' : 'status'}>
            {note.text}
          </p>
        )}
      </li>
    )
  }

  return (
    <div className="page inbox" data-component="Inbox">
      <header className="inbox-head">
        <h1>Inbox</h1>
        {chip('waiting', 'Waiting on you', waitingN, 'attention')}
        {chip('broken', 'Broken', brokenN, 'bad')}
        {chip('done', 'Done today', doneN, 'neutral')}
        <span className="inbox-keys faint"><Kbd keys="j" /><Kbd keys="k" />move <Kbd keys="↵" />open</span>
      </header>
      {kind && (
        <p className="faint">
          Showing {KIND_LABEL[kind as WaitingItem['kind']] ?? kind} only. <a href={href.inbox()}>Show all</a>
        </p>
      )}
      {!loaded && <LoadingRows />}
      {loaded && all.length === 0 && <EmptyState title="Nothing waits on you." />}
      {loaded && items.length === 0 && all.length > 0 && <p className="faint">Nothing of this kind waits on you.</p>}

      {[...GROUPS, OTHER].map((g) => {
        const rows = items.filter((it) => (g.key === 'other' ? !GROUPED.has(it.kind) : g.kinds.includes(it.kind)))
        if (rows.length === 0 || !shown(g.key === 'broken' ? 'broken' : 'waiting')) return null
        return (
          <section key={g.key} className="inbox-group" aria-labelledby={`inbox-${g.key}`}>
            <div role="heading" aria-level={2} className="inbox-h"><GroupHeader id={`inbox-${g.key}`} label={<>{g.title} <span className="faint">{rows.length}</span></>} warn={g.key !== 'broken'} /></div>
            <ul className="inbox-list">{rows.map(row)}</ul>
          </section>
        )
      })}

      {shown('done') && (merged.length > 0 || answered > 0) && (
        <section className="inbox-group" aria-labelledby="inbox-done">
          <div role="heading" aria-level={2} className="inbox-h"><GroupHeader id="inbox-done" label="Done today" /></div>
          <ul className="inbox-list">
            {merged.map((l) => (
              <li key={l.id} className="inbox-item">
                <ListRow
                  glyph="◆"
                  tone="ok"
                  glyphLabel="merged"
                  title={l.mr ? `!${l.mr} ${l.branch}` : l.branch}
                  sub={`${l.repo} · Merged`}
                  age={since(l.closed, now)}
                  href={href.lane(l.id)}
                />
              </li>
            ))}
          </ul>
          {answered > 0 && (
            <p className="faint inbox-more">
              {answered} answered. <a href={href.timeline()}>Show them</a>
            </p>
          )}
        </section>
      )}
    </div>
  )
}
