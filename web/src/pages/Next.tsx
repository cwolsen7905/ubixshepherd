import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { ApiError, nextUp } from '../api/client'
import type { MatrixRow, NextAddBody, NextItem, NextUpdateBody } from '../api/types'
import { Glyph } from '../components/Glyph'
import { since } from '../format'
import { useNow } from '../hooks'
import { prefillDraft, draftKey } from '../model/chatDraft'
import {
  NEXT_MARKER, NEXT_MARKS, NEXT_SECTION_NAMES, canMove, canStart, closedCount, deskWorkspace, lastPlanId, matrixGroups,
  nextSections, planLabel, stateActions, startText,
} from '../model/next'
import { go, href, useFocusTarget, useRoute } from '../router'
import { useSnapshot } from '../state/context'
import { loadNext, useNextList, type LoadNext } from '../state/next'
import '../styles/next.css'

export interface NextApi {
  add: (body: NextAddBody) => Promise<unknown>
  update: (id: number, body: NextUpdateBody) => Promise<unknown>
}

const realApi: NextApi = { add: nextUp.add, update: nextUp.update }

const reason = (e: unknown) => (e instanceof ApiError || e instanceof Error ? e.message : String(e))

const nextSelector = (id: number) => `[data-next-id="${id}"]`

/** The next-up list: what is planned and not yet being acted on, in the person's order. */
export function NextPage({ load = loadNext, api = realApi }: { load?: LoadNext; api?: NextApi }) {
  const snap = useSnapshot()
  const now = useNow()
  const [closed, setClosed] = useState(false)
  const { list, error, reload } = useNextList(closed, load)
  // A plan event on the feed (an item added, approved, deferred, dropped or done) reads
  // the list again at once; the hook's own poll still catches matrix rows, which change
  // with git and not on the feed.
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
  const items = useMemo(() => list?.items ?? [], [list])
  const matrix = useMemo(() => matrixGroups(list), [list])
  const sections = useMemo(() => nextSections(items, closed), [items, closed])
  const lit = useFocusTarget(
    focus,
    sections.some((s) => s.items.some((it) => it.id === focus)),
    nextSelector,
  )
  const hiddenClosed = closed ? 0 : closedCount(items)
  const repos = useMemo(() => [...new Set(snap.lanes.map((l) => l.repo))].sort(), [snap.lanes])
  const workspace = deskWorkspace(snap.status?.workspaces ?? [])

  const act = async (id: number | 'add', run: () => Promise<unknown>) => {
    setBusy(id)
    setProblem('')
    try {
      await run()
      reload()
    } catch (e) {
      setProblem(reason(e))
    } finally {
      setBusy(null)
    }
  }
  const start = (it: NextItem) => {
    prefillDraft(window.localStorage, draftKey(workspace), startText(it))
    go(href.chat())
  }

  const waiting = items.filter((it) => it.stored_state === 'approved' && it.state === 'approved').length
  return (
    <div className="page next">
      <header className="page-head">
        <h1>Next up</h1>
        <p className="page-sub">
          {list === null
            ? 'Reading the list…'
            : `${waiting} approved and not started. Your order is the priority: the first approved item with no lane is next. Only you approve, defer or drop.`}
        </p>
      </header>

      {error && (
        <p className="next-problem tone-bad" role="alert">
          Could not read the list: {error}. {list ? 'Showing what was last loaded.' : 'Retrying.'}
        </p>
      )}
      {problem && (
        <p className="next-problem tone-bad" role="alert">
          {problem}
        </p>
      )}

      <AddForm repos={repos} busy={busy === 'add'} onAdd={(b) => act('add', () => api.add(b))} />

      <p className="next-tools">
        <label>
          <input type="checkbox" checked={closed} onChange={(e) => setClosed(e.target.checked)} /> Show done and dropped
          {hiddenClosed > 0 && <span className="faint"> ({hiddenClosed})</span>}
        </label>
      </p>

      {list !== null && items.length === 0 && matrix.length === 0 && (
        <p className="empty">Nothing is planned. Add an item above, or run <code>shepherd next seed</code>.</p>
      )}

      {sections.map((s) =>
        s.items.length === 0 ? null : (
          <section key={s.state} className={`next-section next-${s.state}`} aria-labelledby={`next-${s.state}-h`}>
            <h2 id={`next-${s.state}-h`} className="section-h">
              {NEXT_SECTION_NAMES[s.state]} <span className="faint">{s.items.length}</span>
            </h2>
            <ol className="next-rows">
              {s.items.map((it) => (
                <NextRow
                  key={it.id}
                  it={it}
                  all={items}
                  isNext={list?.next_up === it.id}
                  focused={lit === it.id}
                  now={now}
                  busy={busy === it.id}
                  lanes={snap.lanes.filter((l) => l.state === 'open' && (!it.repo || l.repo === it.repo))}
                  onUpdate={(b) => act(it.id, () => api.update(it.id, b))}
                  onStart={() => start(it)}
                />
              ))}
            </ol>
          </section>
        ),
      )}

      {matrix.length > 0 && (
        <section className="next-section next-matrix" aria-labelledby="next-matrix-h">
          <h2 id="next-matrix-h" className="section-h">
            From work matrix <span className="faint">{matrix.reduce((n, g) => n + g.rows.length, 0)}</span>
          </h2>
          <p className="faint">
            Rows with status todo or blocked in each repo's <code>docs/WORK.md</code>. Read from git, not stored here: edit the
            file to change one.
          </p>
          {matrix.map((g) => (
            <div key={g.repo}>
              <h3 className="next-matrix-repo">{g.repo}</h3>
              <ol className="next-rows">
                {g.rows.map((r) => (
                  <MatrixItem key={r.key} row={r} />
                ))}
              </ol>
            </div>
          ))}
        </section>
      )}
    </div>
  )
}

/** A work-matrix row: read-only, so it has links and no actions. */
function MatrixItem({ row }: { row: MatrixRow }) {
  const blocked = row.state === 'blocked'
  return (
    <li className={`next-row state-${row.state} next-matrix-row`}>
      <span className="next-glyph">
        <Glyph glyph={blocked ? '■' : '○'} tone={blocked ? 'warn' : 'muted'} label={blocked ? 'blocked' : 'todo'} />
      </span>
      <div className="next-main">
        <p className="next-title">
          <span className="next-id faint">{row.row_id}</span> {row.title}
          {blocked && <span className="badge tone-warn next-badge">blocked</span>}
          <span className="badge next-badge" title="Read through from docs/WORK.md">matrix</span>
        </p>
        <p className="next-meta faint">
          {row.area && <span>{row.area}</span>}
          {row.lane && <span title="The lane the row names">lane {row.lane}</span>}
          {row.mr &&
            (row.pr_url ? (
              <a href={row.pr_url} target="_blank" rel="noreferrer">
                PR {row.mr}
              </a>
            ) : (
              <span>PR {row.mr}</span>
            ))}
        </p>
        {row.note && <p className="next-note">{row.note}</p>}
      </div>
    </li>
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
    <form className="next-add" onSubmit={(e) => void submit(e)} aria-label="Add an item">
      <label className="next-add-title">
        <span className="visually-hidden">Title</span>
        <input type="text" value={title} placeholder="Add an item: what should be done" onChange={(e) => setTitle(e.target.value)} />
      </label>
      <label>
        <span className="visually-hidden">Repo</span>
        <input type="text" list="next-repos" value={repo} placeholder="repo (optional)" onChange={(e) => setRepo(e.target.value)} />
        <datalist id="next-repos">
          {repos.map((r) => (
            <option key={r} value={r} />
          ))}
        </datalist>
      </label>
      <label>
        <span className="visually-hidden">Plan doc</span>
        <input type="text" value={plan} placeholder="docs/plan.md (optional)" onChange={(e) => setPlan(e.target.value)} />
      </label>
      <label className="next-add-approve">
        <input type="checkbox" checked={approve} onChange={(e) => setApprove(e.target.checked)} /> Approved
      </label>
      <button type="submit" className="button button-primary" disabled={busy || !title.trim()}>
        {busy ? 'Adding…' : 'Add'}
      </button>
    </form>
  )
}

function NextRow({
  it, all, isNext, focused, now, busy, lanes, onUpdate, onStart,
}: {
  it: NextItem
  all: NextItem[]
  isNext: boolean
  focused: boolean
  now: number
  busy: boolean
  lanes: { id: number; name: string; repo: string }[]
  onUpdate: (b: NextUpdateBody) => void
  onStart: () => void
}) {
  const mark = NEXT_MARKS[it.state]
  const movable = it.stored_state === 'approved' || it.stored_state === 'planned' || it.stored_state === 'deferred'
  const laneName = it.lane_name ?? it.lane
  return (
    <li
      className={`next-row state-${it.state}${isNext ? ' is-next' : ''}${focused ? ' is-focus-target' : ''}`}
      data-next-id={it.id}
      aria-current={isNext || focused ? 'true' : undefined}
    >
      <span className="next-glyph">
        {isNext ? (
          <Glyph glyph={NEXT_MARKER.glyph} tone={NEXT_MARKER.tone} label={NEXT_MARKER.label} />
        ) : (
          <Glyph glyph={mark.glyph} tone={mark.tone} label={mark.label} />
        )}
      </span>
      <div className="next-main">
        <p className="next-title">
          <span className="next-id faint">#{it.id}</span> {it.title}
          {isNext && <span className="badge tone-accent next-badge">next</span>}
          {it.proposed && <span className="badge tone-warn next-badge" title="Added by an agent or the desk; not yet approved">proposed</span>}
          {it.source === 'chat' && it.state === 'planned' && (
            <span className="badge tone-muted next-badge" title="Registered by the front desk from chat; planned, not waiting on you">chat</span>
          )}
          {it.state === 'started' && <span className="badge tone-accent next-badge">started{it.running ? ', running' : ''}</span>}
          {it.state === 'done' && <span className="badge tone-ok next-badge">done</span>}
        </p>
        <p className="next-meta faint">
          {it.repo && <span>{it.repo}</span>}
          {it.plan && (
            <span title={it.plan}>
              plan <code>{planLabel(it.plan)}</code>
            </span>
          )}
          {it.lane_id ? (
            <a href={href.lane(it.lane_id)}>lane {laneName}</a>
          ) : (
            laneName && <span title="The name a lane opened for this item should have">lane {laneName}</span>
          )}
          {it.mr &&
            (it.mr_url ? (
              <a href={it.mr_url} target="_blank" rel="noreferrer" aria-label={`merge request ${it.mr}${it.mr_state ? `, ${it.mr_state}` : ''}`}>
                MR {it.mr}
                {it.mr_state ? ` ${it.mr_state}` : ''}
              </a>
            ) : (
              <span>
                MR {it.mr}
                {it.mr_state ? ` ${it.mr_state}` : ''}
              </span>
            ))}
          <time dateTime={it.created} title={it.created}>
            {since(it.created, now) || 'now'} old
          </time>
          {it.source !== 'person' && <span>from {it.source}</span>}
        </p>
        {it.why && <p className="next-why tone-warn">{it.why}</p>}
        {it.note && <p className="next-note">{it.note}</p>}
      </div>
      <div className="next-actions" role="group" aria-label={`Actions for ${it.title}`}>
        {canStart(it) && (
          <button type="button" className="button" disabled={busy} onClick={onStart} title="Put a request to open a lane in the chat draft">
            Start
          </button>
        )}
        {stateActions(it).map((a) => (
          <button
            key={a.state}
            type="button"
            className={a.state === 'approved' ? 'button button-primary' : 'button'}
            disabled={busy}
            onClick={() => onUpdate({ state: a.state })}
          >
            {a.label}
          </button>
        ))}
        {movable && (
          <>
            <button type="button" className="button next-move" disabled={busy || !canMove(all, it, 'up')} aria-label={`Move ${it.title} up`} onClick={() => onUpdate({ move: 'up' })}>
              <span aria-hidden="true">↑</span>
            </button>
            <button type="button" className="button next-move" disabled={busy || !canMove(all, it, 'down')} aria-label={`Move ${it.title} down`} onClick={() => onUpdate({ move: 'down' })}>
              <span aria-hidden="true">↓</span>
            </button>
          </>
        )}
        {!it.lane_id && movable && lanes.length > 0 && (
          <label className="next-link">
            <span className="visually-hidden">Link {it.title} to a lane</span>
            <select
              value=""
              disabled={busy}
              onChange={(e) => e.target.value && onUpdate({ lane_id: Number(e.target.value) })}
            >
              <option value="">Link to lane…</option>
              {lanes.map((l) => (
                <option key={l.id} value={l.id}>
                  {l.name}
                </option>
              ))}
            </select>
          </label>
        )}
      </div>
    </li>
  )
}
