import { useEffect, useMemo, useRef, useState } from 'react'
import { history } from '../api/client'
import type { LaneRecord } from '../api/types'
import { RangeFilter } from '../components/RangeFilter'
import { DEFAULT_RANGE, ending, finished, RANGE_PHRASE, rangeStart } from '../model/history'
import { useHistory } from '../state/history'
import type { Range } from '../router'
import { canDismiss, counts, countsText, GROUP_NAMES, GROUPS, type BoardItem, type Group } from '../model/board'
import {
  collapseKey, groupByProject, type ProjectGroup, readCollapsed, readView, summaryText, writeCollapsed, writeView, type BoardView,
} from '../model/projects'
import { useProjectList } from '../state/projects'
import { projectHref } from './Projects'
import '../styles/projects.css'
import '../styles/pages/board.css'
import { Button, Chip, EmptyState, Popover } from '../ui'
import { GROUP_MARKS } from '../model/groups'
import { Glyph } from '../components/Glyph'
import { FinishedMark, LaneCloseButton, useFinishedLanes } from '../components/LaneClose'
import { GateBadge } from '../components/GateBadge'
import { MrBadge } from '../components/MrBadge'
import { RunFailure } from '../components/RunFailure'
import { runCost, since, usd } from '../format'
import { useKeys, useNow } from '../hooks'
import { go, href } from '../router'
import { isOpenNow, itemOpen, openClick, openHref, openRow } from '../components/BacklogOpen'
import { useLive, useSnapshot } from '../state/context'


const sentence = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)

/** The model id shown under the agent: drop any provider/vendor prefix before the last '/'. */
function shortModel(model: string): string {
  const i = model.lastIndexOf('/')
  return i >= 0 ? model.slice(i + 1) : model
}

function AgentCell({ agent, model }: { agent?: string; model?: string }) {
  const short = model ? shortModel(model) : null
  return (
    <span className="row-agent">
      <span className="row-agent-name">{agent ?? ''}</span>
      {short && <span className="row-model" title={model}>{short}</span>}
    </span>
  )
}

/** A project with no open lane and nothing on the board has nothing to show: it folds. */
const isQuiet = (g: ProjectGroup) => g.items.length === 0 && g.summary.open === 0

/** Dismissed runs: how many are hidden, whether they show, and the toggle. */
export interface DismissedControl {
  count: number
  show: boolean
  onShow: (on: boolean) => void
}

export function BoardPage({
  items, range = DEFAULT_RANGE, dismissed,
}: { items: BoardItem[]; range?: Range; dismissed?: DismissedControl }) {
  const snap = useSnapshot()
  const live = useLive()
  const now = useNow()
  const [sel, setSel] = useState(0)
  const rows = useRef<(HTMLAnchorElement | HTMLDivElement | null)[]>([])

  const closed = useHistory(range, async (ids) => {
    const since0 = rangeStart(range, Date.now()).toISOString()
    return (await Promise.all(ids.map((id) => history.lanes(id, since0)))).flat()
  })
  const done = useMemo(() => finished(closed.data ?? [], rangeStart(range, now)), [closed.data, range, now])

  const projects = useProjectList()
  const [view, setView] = useState<BoardView | null>(() => readView(window.localStorage))
  const [collapsed, setCollapsed] = useState(() => readCollapsed(window.localStorage))
  const list = projects.data?.projects
  // Grouping needs a project to group by; Unassigned alone is the flat board.
  const hasProjects = !!list?.some((p) => !p.unassigned)
  const grouped = hasProjects && (view ?? 'grouped') === 'grouped'
  const groups = useMemo(() => (grouped && list ? groupByProject(items, list) : []), [grouped, list, items])
  const [quietOpen, setQuietOpen] = useState(false)
  const quiet = useMemo(() => groups.filter(isQuiet), [groups])
  const active = useMemo(() => groups.filter((g) => !isQuiet(g)), [groups])
  const shown = grouped ? active.filter((g) => !collapsed.has(collapseKey(g))).reduce((n, g) => n + g.items.length, 0) : items.length
  const total = shown + done.length
  const choose = (v: BoardView) => {
    setView(v)
    writeView(window.localStorage, v)
  }
  const [dismissError, setDismissError] = useState<string | null>(null)
  const dismiss = (runId: number, undo = false) =>
    live.dismiss(runId, !undo).then(
      () => setDismissError(null),
      (e) => setDismissError(e instanceof Error ? e.message : String(e)),
    )
  const toggle = (key: string) => {
    const next = new Set(collapsed)
    if (!next.delete(key)) next.add(key)
    setCollapsed(next)
    writeCollapsed(window.localStorage, next)
  }

  // Which open lanes the daemon judges finished, for the marker and the close button.
  const finishedLanes = useFinishedLanes(
    useMemo(() => items.flatMap((it) => (it.lane && !it.decision && !it.request ? [it.lane] : [])), [items]),
    snap.feed.at(-1)?.id ?? 0,
  )

  const clamped = Math.min(sel, Math.max(0, total - 1))
  useEffect(() => {
    rows.current[clamped]?.focus({ preventScroll: false })
  }, [clamped])

  useKeys(
    {
      j: () => setSel((s) => Math.min(s + 1, total - 1)),
      ArrowDown: (e) => (e.preventDefault(), setSel((s) => Math.min(s + 1, total - 1))),
      k: () => setSel((s) => Math.max(s - 1, 0)),
      ArrowUp: (e) => (e.preventDefault(), setSel((s) => Math.max(s - 1, 0))),
      c: () => live.clearDone(),
    },
    [total, live],
  )

  if (!snap.ready) {
    return <p className="empty">{snap.error ? 'Waiting for the daemon.' : 'Loading lanes…'}</p>
  }

  let index = -1
  const renderGroups = (its: BoardItem[]) => {
    const byGroup = new Map<Group, BoardItem[]>()
    for (const it of its) byGroup.set(it.group, [...(byGroup.get(it.group) ?? []), it])
    return GROUPS.flatMap((g) => {
      const list = byGroup.get(g)
      return list ? [{ g, list }] : []
    }).map(({ g, list }) => (
        <section key={g} className={`group group-${g}`} aria-labelledby={`group-${g}`}>
          <h2 id={`group-${g}`} className="section-h group-head">
            <Glyph glyph={GROUP_MARKS[g].glyph} tone={GROUP_MARKS[g].tone} label="" />
            <span className="group-name">{sentence(GROUP_NAMES[g])}</span>
            <span className="group-count">{list.length}</span>
            {g === 'done' && (
              <Button variant="ghost" size="small" onClick={() => live.clearDone()}>
                Mark all seen <kbd>c</kbd>
              </Button>
            )}
          </h2>
          <ul className="rows">
            {list.map((it) => {
              const i = ++index
              const op = itemOpen(it)
              const mark = GROUP_MARKS[it.group]
              const dr = it.run
              const body = (
                <>
                  <Glyph glyph={it.glyph ?? mark.glyph} tone={mark.tone} label={sentence(GROUP_NAMES[it.group])} />
                  <span className="row-lane">{it.name}</span>
                  <AgentCell agent={it.agent} model={it.run?.model} />
                  <span className="row-repo">{it.repo}</span>
                  <span className="row-mr">{it.lane && <MrBadge lane={it.lane} link={false} />}{it.lane?.gate && <> <GateBadge gate={it.lane.gate} /></>}</span>
                  <span className="row-detail">{it.detail}</span>
                  <span className="row-age" title={it.at}>
                    {since(it.at, now)}
                  </span>
                </>
              )
              return (
                <li key={it.key}>
                  {op || it.request ? (
                    <a
                      ref={(el) => {
                        rows.current[i] = el
                      }}
                      className="row"
                      href={op ? openHref(op) : href.decisions()}
                      aria-current={op && isOpenNow(op) ? 'true' : undefined}
                      data-selected={i === clamped ? 'true' : undefined}
                      onFocus={() => setSel(i)}
                      onClick={op ? openClick(op) : (e) => (e.preventDefault(), go(href.decisions()))}
                    >
                      {body}
                    </a>
                  ) : (
                    <div
                      ref={(el) => {
                        rows.current[i] = el
                      }}
                      className="row"
                      tabIndex={0}
                      onFocus={() => setSel(i)}
                    >
                      {body}
                    </div>
                  )}
                  {it.lane && it.lane.state === 'open' && !it.decision && !it.request && (
                    <p className="row-actions lane-close-row">
                      {(() => {
                        const review = finishedLanes.get(it.lane.id)
                        return review && <FinishedMark review={review} lastRun={it.run?.state} />
                      })()}
                      <Popover label="…">
                        <LaneCloseButton lane={it.lane} running={it.run?.state === 'running'} />
                      </Popover>
                    </p>
                  )}
                  {dr && (canDismiss(it) || (dismissed?.show && dr.dismissed)) && (
                    <p className="row-actions">
                      {dr.dismissed ? (
                        <button type="button" className="link-button" onClick={() => void dismiss(dr.id, true)}>
                          Undismiss run {dr.id}
                        </button>
                      ) : (
                        <button
                          type="button"
                          className="link-button"
                          title="Clear this run from the board. Its log and record stay in history."
                          onClick={() => void dismiss(dr.id)}
                        >
                          Dismiss run {dr.id}
                        </button>
                      )}
                    </p>
                  )}
                  {it.group === 'broken' && it.run?.diagnosis && (
                    <RunFailure run={it.run} compact onFixed={() => void live.refresh()} />
                  )}
                </li>
              )
            })}
          </ul>
        </section>
    ))
  }

  return (
    <div className="board">
      <header className="page-head">
        <h1>Board</h1>
        <p className="page-sub">{countsText(counts(items)) || 'Nothing needs you right now.'}</p>
        <div className="page-actions">
          {hasProjects && (
            <span className="board-view" role="group" aria-label="View">
              <Chip kind="filter" selected={grouped} onClick={() => choose('grouped')}>
                By project
              </Chip>
              <Chip kind="filter" selected={!grouped} onClick={() => choose('flat')}>
                Flat
              </Chip>
            </span>
          )}
          {dismissed && (dismissed.count > 0 || dismissed.show) && (
            <Chip kind="filter" selected={dismissed.show} onClick={() => dismissed.onShow(!dismissed.show)}>
              {dismissed.show ? 'Hide dismissed' : 'Show dismissed'}
              {dismissed.count > 0 ? ` (${dismissed.count})` : ''}
            </Chip>
          )}
        </div>
      </header>
      {dismissError && <p className="group-note tone-bad">Could not change the run: {dismissError}</p>}
      {items.length === 0 && !grouped && (
        <EmptyState title="Nothing needs you, nothing is running, and nothing finished since you last looked.">
          Start work from the terminal: <code>shepherd lane run &lt;lane&gt; --agent claude</code>.
        </EmptyState>
      )}
      {grouped
        ? active.map((g) => {
            const key = collapseKey(g)
            const open = !collapsed.has(key)
            const id = `project-${g.unassigned ? 'unassigned' : g.name}`
            return (
              <section key={key} className="project-group" aria-labelledby={id}>
                <h2 id={id} className="section-h project-group-head">
                  <button type="button" className="project-toggle" aria-expanded={open} onClick={() => toggle(key)}>
                    <span className="caret" aria-hidden="true">
                      {open ? '▾' : '▸'}
                    </span>
                    {g.title}
                  </button>
                  <span className="project-summary">{summaryText(g.summary, (n) => usd(n) || '$0.00')}</span>
                  <a
                    href={projectHref({ name: g.name, unassigned: g.unassigned })}
                    onClick={(e) => {
                      e.preventDefault()
                      go(projectHref({ name: g.name, unassigned: g.unassigned }))
                    }}
                  >
                    Open
                  </a>
                </h2>
                {open && (
                  <div className="project-group-body">
                    {g.items.length === 0 ? <p className="project-group-empty faint">Nothing needs attention here.</p> : renderGroups(g.items)}
                  </div>
                )}
              </section>
            )
          })
        : renderGroups(items)}
      {grouped && quiet.length > 0 && (
        <section className="quiet-projects" aria-label="Quiet projects">
          <h2 className="section-h">
            <button
              type="button"
              className="project-toggle"
              aria-expanded={quietOpen}
              onClick={() => setQuietOpen((o) => !o)}
            >
              <span className="caret" aria-hidden="true">
                {quietOpen ? '▾' : '▸'}
              </span>
              {quiet.length} quiet {quiet.length === 1 ? 'project' : 'projects'}
            </button>
          </h2>
          {quietOpen && (
            <ul className="quiet-list">
              {quiet.map((g) => {
                const to = projectHref({ name: g.name, unassigned: g.unassigned })
                return (
                  <li key={collapseKey(g)}>
                    <a
                      href={to}
                      onClick={(e) => {
                        e.preventDefault()
                        go(to)
                      }}
                    >
                      {g.title}
                    </a>
                    <span className="faint">{summaryText(g.summary, (n) => usd(n) || '$0.00')}</span>
                  </li>
                )
              })}
            </ul>
          )}
        </section>
      )}
      <FinishedSection
        lanes={done}
        range={range}
        loading={closed.loading && closed.data === null}
        error={closed.error}
        offset={shown}
        now={now}
        selected={clamped}
        onSelect={setSel}
        rows={rows}
      />
    </div>
  )
}

/** Closed lanes, newest first: what ran and how it ended. Rows go on after the board's own. */
function FinishedSection({
  lanes, range, loading, error, offset, now, selected, onSelect, rows,
}: {
  lanes: LaneRecord[]
  range: Range
  loading: boolean
  error: string | null
  offset: number
  now: number
  selected: number
  onSelect: (i: number) => void
  rows: React.MutableRefObject<(HTMLAnchorElement | HTMLDivElement | null)[]>
}) {
  const phrase = RANGE_PHRASE[range]
  return (
    <section className="group group-finished" aria-labelledby="group-finished">
      <h2 id="group-finished" className="section-h group-head">
        <Glyph glyph="−" tone="muted" label="" />
        <span className="group-name">Finished</span>
        <span className="group-count">{lanes.length}</span>
        <RangeFilter range={range} href={href.board} />
      </h2>
      {error && <p className="group-note tone-bad">Could not load closed lanes: {error}</p>}
      {loading && !error && <p className="group-note faint">Loading…</p>}
      {!loading && !error && lanes.length === 0 && <p className="group-note faint">No lane closed {phrase}.</p>}
      {lanes.length > 0 && (
        <ul className="rows">
          {lanes.map((l, n) => {
            const i = offset + n
            const end = ending(l)
            const cost = runCost(l) || usd(0)
            return (
              <li key={l.id}>
              <div {...openRow({ kind: 'lane', id: l.id })} className="row row-finished" tabIndex={-1} data-selected={i === selected ? 'true' : undefined}>
                <Glyph glyph="−" tone={end.tone} label="closed" />
                <span className="row-lane">
                  <a
                    ref={(el) => {
                      rows.current[i] = el
                    }}
                    className="row-link"
                    href={openHref({ kind: 'lane', id: l.id })}
                    onFocus={() => onSelect(i)}
                    onClick={openClick({ kind: 'lane', id: l.id })}
                  >
                    {l.name}
                  </a>
                </span>
                <span className="row-repo">{l.repo}</span>
                <span className="row-end" title={end.title}>
                  <span className={`tone-${end.tone}`}>{end.text}</span>
                  {l.mr ? (
                    <>
                      {' '}
                      {l.mr_url ? (
                        <a className="row-mr-link" href={l.mr_url} target="_blank" rel="noreferrer">
                          !{l.mr}
                        </a>
                      ) : (
                        <span>!{l.mr}</span>
                      )}
                    </>
                  ) : null}
                </span>
                <span className="row-runs">
                  {l.runs} {l.runs === 1 ? 'run' : 'runs'}
                </span>
                <span className="row-cost">{cost}</span>
                <span className="row-age" title={l.closed}>
                  {since(l.closed, now)} ago
                </span>
              </div>
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}
