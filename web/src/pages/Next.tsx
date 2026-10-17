import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { ApiError, nextUp } from '../api/client'
import type { MatrixRow, NextAddBody, NextItem as ApiNextItem, NextUpdateBody } from '../api/types'
import { since } from '../format'
import { useKeys, useNow } from '../hooks'
import { prefillDraft, draftKey } from '../model/chatDraft'
import {
  NEXT_MARKER, NEXT_MARKS, NEXT_SECTION_NAMES, canMove, canStart, closedCount, deskWorkspace, lastPlanId, matrixGroups,
  nextSections, planLabel, stateActions, startText,
} from '../model/next'
import { go, href, useFocusTarget, useRoute } from '../router'
import { useSnapshot } from '../state/context'
import { loadNext, useNextList, type LoadNext } from '../state/next'
import { Button, Chip, EmptyState, GroupHeader } from '../ui'
import { isInner, openInInspector, openLink } from '../components/BacklogOpen'
import './Next.css'

export interface NextApi {
  add: (body: NextAddBody) => Promise<unknown>
  update: (id: number, body: NextUpdateBody) => Promise<unknown>
}

const realApi: NextApi = { add: nextUp.add, update: nextUp.update }

const reason = (e: unknown) => (e instanceof ApiError || e instanceof Error ? e.message : String(e))

const nextSelector = (id: number) => `[data-next-id="${id}"]`

interface NextBlocker {
  id: number
  title: string
  state: string
}

/** The daemon already returns these; the shared api/types.ts will catch up in its own lane. */
interface NextItem extends ApiNextItem {
  depends_on?: number[]
  blocked_on?: NextBlocker[]
}

function blockedOn(it: ApiNextItem): NextBlocker[] {
  return (it as NextItem).blocked_on ?? []
}

/** The next-up list: what is planned and not yet being acted on, in the person's order. */
export function NextPage({ load = loadNext, api = realApi }: { load?: LoadNext; api?: NextApi }) {
  const snap = useSnapshot()
  const now = useNow()
  const [closed, setClosed] = useState(false)
  const { list, error, reload } = useNextList(closed, load)
  const planLast = lastPlanId(snap.feed)
  const seenPlan = useRef(planLast)
  useEffect(() => {
    if (planLast !== seenPlan.current) {
      seenPlan.current = planLast
      reload()
    }
  }, [planLast, reload])
  const route = useRoute()
  const focus = route.page === 'next' ? route.focus : undefined
  const [busy, setBusy] = useState<number | 'add' | null>(null)
  const [problem, setProblem] = useState('')
  const items = useMemo(() => (list?.items as NextItem[] | undefined) ?? [], [list])
  const matrix = useMemo(() => matrixGroups(list), [list])
  const sections = useMemo(() => nextSections(items, closed), [items, closed])
  const lit = useFocusTarget(
    focus,
    sections.some((s) => s.items.some((it) => it.id === focus)),
    nextSelector,
  )
  const proposed = useMemo(() => items.filter((it) => it.proposed && it.stored_state === 'planned'), [items])
  const queue = useMemo(() => sections.map((s) => ({ ...s, items: s.items.filter((it) => !proposed.includes(it)) })), [sections, proposed])
  const queueItems = useMemo(() => queue.flatMap((s) => s.items), [queue])
  const hiddenClosed = closed ? 0 : closedCount(items)
  const repos = useMemo(() => [...new Set(snap.lanes.map((l) => l.repo))].sort(), [snap.lanes])
  const workspace = deskWorkspace(snap.status?.workspaces ?? [])
  const [selected, setSelected] = useState<number | null>(null)
  const [seenFocus, setSeenFocus] = useState<number | undefined>()
  if (focus !== seenFocus && (!focus || items.some((it) => it.id === focus))) {
    setSeenFocus(focus)
    if (focus) setSelected(focus)
  }
  const selectedItem = useMemo(() => items.find((it) => it.id === selected) ?? null, [items, selected])
  const selectedPeers = useMemo(() => {
    if (!selectedItem) return []
    return queueItems.filter((it) => it.stored_state === selectedItem.stored_state)
  }, [queueItems, selectedItem])

  const act = async (id: number | 'add', run: () => Promise<unknown>): Promise<boolean> => {
    setBusy(id)
    setProblem('')
    try {
      await run()
      reload()
      return true
    } catch (e) {
      setProblem(reason(e))
      return false
    } finally {
      setBusy(null)
    }
  }
  const start = (it: NextItem) => {
    prefillDraft(window.localStorage, draftKey(workspace), startText(it))
    go(href.chat())
  }

  useKeys(
    {
      Escape: () => setSelected(null),
      j: () => moveCursor(1),
      k: () => moveCursor(-1),
    },
    [items, selected],
  )

  function moveCursor(dir: number) {
    const flat = proposed.concat(queueItems)
    if (!flat.length) return
    const idx = selected ? flat.findIndex((it) => it.id === selected) : -1
    const next = Math.max(0, Math.min(flat.length - 1, idx + dir))
    setSelected(flat[next]?.id ?? null)
  }

  const renderRow = (peers: NextItem[]) => (it: NextItem) => (
    <NextRow
      key={it.id}
      it={it}
      all={peers}
      isNext={list?.next_up === it.id}
      focused={lit === it.id}
      selected={selected === it.id}
      now={now}
      onSelect={() => setSelected(it.id)}
      onMove={async (draggedId, dir, steps) => {
        for (let i = 0; i < steps; i++) {
          const ok = await act(draggedId, () => api.update(draggedId, { move: dir }))
          if (!ok) break
        }
      }}
    />
  )

  return (
    <div className={`next-view${selectedItem ? ' has-detail' : ''}`}>
      <header className="next-head">
        <h1>Next up</h1>
        <span className="next-head-right">
          <label className="next-composer-check">
            <input type="checkbox" checked={closed} onChange={(e) => setClosed(e.target.checked)} /> Show done and dropped
            {hiddenClosed > 0 && <span className="sh-faint"> ({hiddenClosed})</span>}
          </label>
        </span>
      </header>

      <div className={`next-body${selectedItem ? ' has-detail' : ''}`}>
        <div className="next-list">
          {error && (
            <p className="sh-conv-problem" role="alert">
              Could not read the list: {error}. {list ? 'Showing what was last loaded.' : 'Retrying.'}
            </p>
          )}
          {problem && (
            <p className="sh-conv-problem" role="alert">
              {problem}
            </p>
          )}

          <AddForm repos={repos} busy={busy === 'add'} onAdd={async (b) => { await act('add', () => api.add(b)) }} />

          {list !== null && items.length === 0 && matrix.length === 0 && (
            <EmptyState title="Nothing is planned">
              Add an item above, or run <code>shepherd next seed</code>.
            </EmptyState>
          )}

          {proposed.length > 0 && (
            <section className="next-section" aria-label="Waiting on you">
              <GroupHeader variant="section" label="Waiting on you" hint={proposed.length} warn />
              <ol className="sh-rows">{proposed.map(renderRow(proposed))}</ol>
            </section>
          )}

          {queue.some((s) => s.items.length > 0) && (
            <section className="next-section" aria-label="Queue">
              <GroupHeader variant="section" label="Queue" hint={queue.reduce((n, s) => n + s.items.length, 0)} />
              {queue.map((s) =>
                s.items.length === 0 ? null : (
                  <div key={s.state}>
                    <GroupHeader variant="section" label={NEXT_SECTION_NAMES[s.state]} hint={s.items.length} />
                    <ol className="sh-rows">{s.items.map(renderRow(queueItems))}</ol>
                  </div>
                ),
              )}
            </section>
          )}

          {matrix.length > 0 && (
            <section className="next-section" aria-label="From work matrix">
              <GroupHeader variant="section" label="From work matrix" hint={matrix.reduce((n, g) => n + g.rows.length, 0)} />
              {matrix.map((g) => (
                <div key={g.repo}>
                  <GroupHeader variant="section" label={g.repo} hint={g.rows.length} />
                  <ol className="sh-rows">
                    {g.rows.map((r) => (
                      <MatrixItem key={r.key} row={r} />
                    ))}
                  </ol>
                </div>
              ))}
            </section>
          )}
        </div>

        {selectedItem && (
          <NextDetail
            it={selectedItem}
            peers={selectedPeers}
            busy={busy === selectedItem.id}
            lanes={snap.lanes.filter((l) => l.state === 'open' && (!selectedItem.repo || l.repo === selectedItem.repo))}
            onClose={() => setSelected(null)}
            onUpdate={(b) => act(selectedItem.id, () => api.update(selectedItem.id, b))}
            onStart={() => start(selectedItem)}
          />
        )}
      </div>
    </div>
  )
}

function AddForm({ repos, busy, onAdd }: { repos: string[]; busy: boolean; onAdd: (b: NextAddBody) => Promise<void> }) {
  const [title, setTitle] = useState('')
  const [repo, setRepo] = useState('')
  const [plan, setPlan] = useState('')
  const [approve, setApprove] = useState(false)
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (!title.trim() || busy) return
    await onAdd({
      title: title.trim(),
      ...(repo.trim() ? { repo: repo.trim() } : {}),
      ...(plan.trim() ? { plan: plan.trim() } : {}),
      state: approve ? 'approved' : 'planned',
    })
    setTitle('')
    setPlan('')
  }
  return (
    <form className="sh-composer next-composer" onSubmit={(e) => void submit(e)} aria-label="Add an item">
      <label className="next-composer-title">
        <span className="visually-hidden">Title</span>
        <input className="sh-input" type="text" value={title} placeholder="Add an item: what should be done" onChange={(e) => setTitle(e.target.value)} />
      </label>
      <label>
        <span className="visually-hidden">Repo</span>
        <input className="sh-input" type="text" list="next-repos" value={repo} placeholder="repo (optional)" onChange={(e) => setRepo(e.target.value)} />
        <datalist id="next-repos">
          {repos.map((r) => (
            <option key={r} value={r} />
          ))}
        </datalist>
      </label>
      <label>
        <span className="visually-hidden">Plan doc</span>
        <input className="sh-input" type="text" value={plan} placeholder="docs/plan.md (optional)" onChange={(e) => setPlan(e.target.value)} />
      </label>
      <label className="next-composer-check">
        <input type="checkbox" checked={approve} onChange={(e) => setApprove(e.target.checked)} /> Approved
      </label>
      <Button type="submit" variant="primary" disabled={busy || !title.trim()}>
        {busy ? 'Adding…' : 'Add'}
      </Button>
    </form>
  )
}

function NextRow({
  it, all, isNext, focused, selected, now, onSelect, onMove,
}: {
  it: NextItem
  all: NextItem[]
  isNext: boolean
  focused: boolean
  selected: boolean
  now: number
  onSelect: () => void
  onMove: (draggedId: number, dir: 'up' | 'down', steps: number) => void
}) {
  const mark = it.proposed ? { glyph: '≡', tone: 'warn' as const, label: 'proposed, waiting on you' } : NEXT_MARKS[it.state]
  const movable = it.stored_state === 'approved' || it.stored_state === 'planned' || it.stored_state === 'deferred'
  const blocked = blockedOn(it)
  const flatIdx = all.findIndex((x) => x.id === it.id)
  const rowRef = useRef<HTMLLIElement>(null)

  const handleDragStart = (e: React.DragEvent) => {
    if (!movable) {
      e.preventDefault()
      return
    }
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', String(it.id))
    rowRef.current?.classList.add('is-dragging')
  }
  const handleDragEnd = () => rowRef.current?.classList.remove('is-dragging')
  const handleDragOver = (e: React.DragEvent) => {
    if (!movable) return
    e.preventDefault()
    e.dataTransfer.dropEffect = 'move'
  }
  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault()
    rowRef.current?.classList.remove('is-dragging')
    if (!movable) return
    const fromId = Number(e.dataTransfer.getData('text/plain'))
    const from = all.findIndex((x) => x.id === fromId)
    if (from < 0 || from === flatIdx) return
    const dir = flatIdx < from ? 'up' : 'down'
    const steps = Math.abs(flatIdx - from)
    onMove(fromId, dir, steps)
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !isInner(e)) {
      e.preventDefault()
      open()
    }
  }
  /** Select the row for its detail panel; a row linked to a lane also opens that lane in the inspector. */
  const open = () => {
    onSelect()
    if (it.lane_id) openInInspector({ kind: 'lane', id: it.lane_id })
  }

  return (
    <li
      ref={rowRef}
      tabIndex={0}
      className={`sh-row next-row${isNext ? ' is-next' : ''}${focused ? ' is-focus-target' : ''}${selected ? ' is-selected' : ''}`}
      data-next-id={it.id}
      draggable={movable}
      onDragStart={handleDragStart}
      onDragEnd={handleDragEnd}
      onDragOver={handleDragOver}
      onDrop={handleDrop}
      onClick={(e) => { if (!isInner(e)) open() }}
      onKeyDown={handleKeyDown}
      aria-current={isNext || focused ? 'true' : undefined}
    >
      <span className="next-row-handle" aria-hidden={!movable} aria-label={movable ? `Drag to reorder ${it.title}` : undefined} title={movable ? 'Drag to reorder' : undefined}>
        {movable ? '≡' : ''}
      </span>
      <span className="sh-glyph" title={mark.label} aria-label={mark.label}>
        {isNext ? NEXT_MARKER.glyph : mark.glyph}
      </span>
      <div className="sh-row-main">
        <p>
          <span className="sh-faint">#{it.id}</span> {it.title}
          {isNext && <Chip kind="filter">next</Chip>}
          {it.proposed && <Chip>proposed</Chip>}
          {it.state === 'started' && <Chip>started</Chip>}
          {it.state === 'done' && <Chip>done</Chip>}
          {it.state === 'dropped' && <Chip>rejected</Chip>}
          {it.stored_state === 'deferred' && it.state !== 'done' && <Chip>deferred</Chip>}
          {it.source === 'chat' && it.state === 'planned' && <Chip>chat</Chip>}
        </p>
        <p className="sh-row-sub">
          {it.repo && <span>{it.repo}</span>}
          {it.plan && (
            <span title={it.plan}>
              plan <code>{planLabel(it.plan)}</code>
            </span>
          )}
          {it.lane_id ? (
            <a {...openLink({ kind: 'lane', id: it.lane_id })}>
              lane {it.lane_name ?? it.lane}
            </a>
          ) : (
            it.lane && <span>lane {it.lane}</span>
          )}
          {it.mr &&
            (it.mr_url ? (
              <a href={it.mr_url} target="_blank" rel="noreferrer" onClick={(e) => e.stopPropagation()}>
                MR {it.mr}
              </a>
            ) : (
              <span>MR {it.mr}</span>
            ))}
          {it.source !== 'person' && <span>from {it.source}</span>}
        </p>
        {blocked.length > 0 && (
          <p>
            <span className="sh-tag sh-tag--blocked">⊘ blocked by {blocked.map((b) => `#${b.id}`).join(', ')}</span>
          </p>
        )}
        {it.why && <p>
            <span className="sh-tag">{it.why}</span>
          </p>}
      </div>
      <span className="sh-row-sub">{since(it.created, now) || 'now'}</span>
    </li>
  )
}

function NextDetail({
  it, peers, busy, lanes, onClose, onUpdate, onStart,
}: {
  it: NextItem
  peers: NextItem[]
  busy: boolean
  lanes: { id: number; name: string; repo: string }[]
  onClose: () => void
  onUpdate: (b: NextUpdateBody) => void
  onStart: () => void
}) {
  const actions = stateActions(it)
  const movable = it.stored_state === 'approved' || it.stored_state === 'planned' || it.stored_state === 'deferred'
  const canMoveUp = canMove(peers, it, 'up')
  const canMoveDown = canMove(peers, it, 'down')
  return (
    <aside className="next-detail" aria-label={`Details for ${it.title}`}>
      <header className="next-detail-head">
        <h2>#{it.id} {it.title}</h2>
        <Button variant="ghost" size="small" onClick={onClose} aria-label="Close details">
          Close
        </Button>
      </header>
      <div className="next-detail-body">
        <p className="sh-who">
          {it.proposed ? 'Proposed' : 'Added'} by <b>{it.source}</b>
        </p>
        <p className="sh-cell-meta">Added {it.created}</p>

        {it.note && (
          <>
            <h3>Note</h3>
            <p>{it.note}</p>
          </>
        )}

        <h3>Would run as</h3>
        <dl className="next-detail-grid">
          <dt>Project</dt>
          <dd>{it.repo || '—'}</dd>
          <dt>Lane</dt>
          <dd>{it.lane_name ?? it.lane ?? '—'}</dd>
          <dt>Plan</dt>
          <dd>{it.plan ? <code>{planLabel(it.plan)}</code> : '—'}</dd>
        </dl>

        {blockedOn(it).length > 0 && (
          <>
            <h3>Blocked by</h3>
            <ul>
              {blockedOn(it).map((b) => (
                <li key={b.id}>
                  #{b.id} {b.title} <span className="sh-cell-meta">({b.state})</span>
                </li>
              ))}
            </ul>
          </>
        )}

        <div className="next-detail-actions">
          {canStart(it) && (
            <Button variant="primary" size="small" disabled={busy} onClick={onStart}>
              Start
            </Button>
          )}
          {it.proposed ? (
            <Button variant="attention" size="small" disabled={busy} onClick={() => onUpdate({ state: 'approved' })}>
              Approve and queue
            </Button>
          ) : (
            actions.map((a) => (
              <Button key={a.state} variant={a.state === 'approved' ? 'attention' : 'secondary'} size="small" disabled={busy} onClick={() => onUpdate({ state: a.state })}>
                {a.label}
              </Button>
            ))
          )}
          {it.proposed && (
            <>
              <Button variant="secondary" size="small" disabled={busy} onClick={() => onUpdate({ state: 'deferred' })}>
                Defer
              </Button>
              <Button variant="secondary" size="small" disabled={busy} onClick={() => onUpdate({ state: 'dropped' })}>
                Drop
              </Button>
            </>
          )}
          {movable && (
            <>
              <Button variant="ghost" size="small" disabled={busy || !canMoveUp} aria-label={`Move ${it.title} up`} onClick={() => onUpdate({ move: 'up' })}>
                ↑
              </Button>
              <Button variant="ghost" size="small" disabled={busy || !canMoveDown} aria-label={`Move ${it.title} down`} onClick={() => onUpdate({ move: 'down' })}>
                ↓
              </Button>
            </>
          )}
          {!it.lane_id && movable && lanes.length > 0 && (
            <select
              className="sh-input"
              disabled={busy}
              value=""
              onChange={(e) => {
                const id = Number(e.target.value)
                if (id > 0) onUpdate({ lane_id: id })
                e.target.value = ''
              }}
              aria-label="Link to lane"
            >
              <option value="">Link to lane…</option>
              {lanes.map((l) => (
                <option key={l.id} value={l.id}>
                  {l.repo}/{l.name}
                </option>
              ))}
            </select>
          )}
        </div>
      </div>
    </aside>
  )
}

function MatrixItem({ row }: { row: MatrixRow }) {
  const blocked = row.state === 'blocked'
  return (
    <li className="sh-row next-row state-matrix">
      <span />
      <span className="sh-glyph" title={blocked ? 'blocked' : 'todo'} aria-label={blocked ? 'blocked' : 'todo'}>
        {blocked ? '■' : '○'}
      </span>
      <div className="sh-row-main">
        <p>
          <span className="sh-faint">{row.row_id}</span> {row.title}
          {blocked && <Chip>blocked</Chip>}
          <Chip>matrix</Chip>
        </p>
        <p className="sh-row-sub">
          {row.area && <span>{row.area}</span>}
          {row.lane && <span>lane {row.lane}</span>}
          {row.mr &&
            (row.pr_url ? (
              <a href={row.pr_url} target="_blank" rel="noreferrer">
                PR {row.mr}
              </a>
            ) : (
              <span>PR {row.mr}</span>
            ))}
        </p>
        {row.note && <p>
            <span className="sh-tag">{row.note}</span>
          </p>}
      </div>
      <span className="sh-row-sub" />
    </li>
  )
}
