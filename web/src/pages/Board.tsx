import { useEffect, useMemo, useRef, useState } from 'react'
import { history } from '../api/client'
import type { LaneRecord } from '../api/types'
import { RangeFilter } from '../components/RangeFilter'
import { DEFAULT_RANGE, ending, finished, RANGE_PHRASE, rangeStart } from '../model/history'
import { useHistory } from '../state/history'
import type { Range } from '../router'
import { canDismiss, counts, countsText, GROUP_NAMES, latestRuns, type BoardItem, type Group } from '../model/board'
import {
  collapseKey, groupByProject, type ProjectGroup, readView, summaryText, writeView, type BoardView,
} from '../model/projects'
import { useProjectList } from '../state/projects'
import { projectHref } from './Projects'
import { useNow, useKeys } from '../hooks'
import { go, href } from '../router'
import { useLive, useSnapshot } from '../state/context'
import { isOpenNow, itemOpen, openHref } from '../components/BacklogOpen'
import { FinishedMark, LaneCloseButton, useFinishedLanes } from '../components/LaneClose'
import { RunFailure } from '../components/RunFailure'
import { runCost, since, usd } from '../format'
import { ListRow, GroupHeader, Chip, EmptyState, Popover } from '../ui'
import { glyphTone } from '../ui/StatusGlyph'
import { GROUP_MARKS } from '../model/groups'
import type { Tone } from '../model/marks'
import { MrBadge } from '../components/MrBadge'
import { GateBadge } from '../components/GateBadge'
import '../styles/projects.css'
import '../styles/pages/board.css'
import './Board.css'

const sentence = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)

/** The model id shown under the agent: drop any provider/vendor prefix before the last '/'. */
function shortModel(model: string): string {
  const i = model.lastIndexOf('/')
  return i >= 0 ? model.slice(i + 1) : model
}

/** Markdown as plain text, for a one-line sub-line: emphasis, code, links, headings and list marks go. */
export function plainText(s: string): string {
  return s
    .replace(/!?\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/(\*\*|__)(.+?)\1/g, '$2')
    .replace(/(^|[\s(])([*_])([^*_\s][^*_]*?)\2(?=[\s).,;:!?]|$)/g, '$1$3')
    .replace(/`([^`]*)`/g, '$1')
    .replace(/^\s{0,3}(#{1,6}|[-*+]|\d+\.)\s+/gm, '')
    .replace(/\s+/g, ' ')
    .trim()
}

function agentSub(it: BoardItem): string {
  const parts: string[] = []
  if (it.agent) parts.push(it.agent)
  if (it.run?.model) parts.push(shortModel(it.run.model))
  parts.push(plainText(it.detail))
  return parts.join(' · ')
}

/**
 * A row of the board. Held and idle lanes are not in the model's groups (they need nothing from
 * anyone); the page adds them, marked by `row`, so the model and the terminal's dock stay as they are.
 */
type Row = BoardItem & { row?: 'held' | 'idle' }
type RowGroup = Group | 'held' | 'idle'

/** Top to bottom: what needs the person first, what needs no one last. */
const ROW_GROUPS: readonly RowGroup[] = ['needs', 'broken', 'review', 'working', 'held', 'idle', 'done']
const ROW_NAMES: Record<RowGroup, string> = { ...GROUP_NAMES, held: 'held', idle: 'idle' }
const ROW_MARKS: Record<RowGroup, { glyph: string; tone: Tone }> = {
  ...GROUP_MARKS,
  held: { glyph: '‖', tone: 'muted' },
  idle: { glyph: '○', tone: 'muted' },
}
const rowGroup = (it: Row): RowGroup => it.row ?? it.group

const clock = (iso: string) => new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false })

/** A project with no open lane and nothing on the board has nothing to show: it folds. */
const isQuiet = (g: ProjectGroup) => g.items.length === 0 && g.summary.open === 0

export interface DismissedControl {
  count: number
  show: boolean
  onShow: (on: boolean) => void
}

export function BoardPage({
  items: listed, range = DEFAULT_RANGE, dismissed,
}: { items: BoardItem[]; range?: Range; dismissed?: DismissedControl }) {
  const snap = useSnapshot()
  const live = useLive()
  const now = useNow()
  const [sel, setSel] = useState(0)
  const quotaHolds = snap.status?.detail?.quota_holds
  const items: Row[] = useMemo(() => {
    const onBoard = new Set(listed.flatMap((it) => (it.laneId === undefined ? [] : [it.laneId])))
    const latest = latestRuns(snap.runs)
    const extra: Row[] = []
    for (const l of snap.lanes) {
      if (l.state !== 'open' || onBoard.has(l.id)) continue
      const run = latest.get(l.id)
      const hold = run && quotaHolds?.find((h) => h.agent === run.agent && Date.parse(h.until) > now)
      extra.push({
        key: `lane:${l.id}`, group: 'done', row: hold ? 'held' : 'idle', name: l.name, laneId: l.id, repo: l.repo,
        agent: run?.agent, lane: l, run, at: run?.ended ?? l.created,
        detail: hold ? `held: out of quota until ${clock(hold.until)}` : 'idle',
      })
    }
    return [...listed, ...extra.sort((a, b) => Date.parse(b.at) - Date.parse(a.at))]
  }, [listed, snap.lanes, snap.runs, quotaHolds, now])
  const rows = useRef<(HTMLAnchorElement | HTMLDivElement | null)[]>([])

  const closed = useHistory(range, async (ids) => {
    const since0 = rangeStart(range, Date.now()).toISOString()
    return (await Promise.all(ids.map((id) => history.lanes(id, since0)))).flat()
  })
  const done = useMemo(() => finished(closed.data ?? [], rangeStart(range, now)), [closed.data, range, now])

  const projects = useProjectList()
  const [view, setView] = useState<BoardView | null>(() => readView(window.localStorage))
  const list = projects.data?.projects
  const hasProjects = !!list?.some((p) => !p.unassigned)
  const grouped = hasProjects && (view ?? 'grouped') === 'grouped'
  const groups = useMemo(() => (grouped && list ? groupByProject(items, list) : []), [grouped, list, items])
  const [quietOpen, setQuietOpen] = useState(false)
  const quiet = useMemo(() => groups.filter(isQuiet), [groups])
  const active = useMemo(() => groups.filter((g) => !isQuiet(g)), [groups])
  const shown = grouped ? active.reduce((n, g) => n + g.items.length, 0) : items.length
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

  const idle = items.filter((it) => it.row === 'idle').length
  const held = items.filter((it) => it.row === 'held').length
  const summary = [
    countsText(counts(items.filter((it) => !it.row))),
    held > 0 ? `${held} held` : '',
    idle > 0 ? `${idle} idle` : '',
  ].filter(Boolean).join(' · ')

  let index = -1
  const renderGroups = (its: Row[]) => {
    const byGroup = new Map<RowGroup, Row[]>()
    for (const it of its) byGroup.set(rowGroup(it), [...(byGroup.get(rowGroup(it)) ?? []), it])
    return ROW_GROUPS.flatMap((g) => {
      const list = byGroup.get(g)
      return list ? [{ g, list }] : []
    }).map(({ g, list }) => {
      const mark = ROW_MARKS[g]
      return (
        <section key={g} className="board-group" aria-labelledby={`group-${g}`}>
          <GroupHeader
            id={`group-${g}`}
            variant="status"
            glyph={mark.glyph}
            glyphTone={glyphTone(mark.tone)}
            label={sentence(ROW_NAMES[g])}
            count={list.length}
            warn={g === 'needs'}
          />
          <ul className="board-rows">
            {list.map((it) => {
              const i = ++index
              const op = itemOpen(it)
              const runId = it.run?.id
              const row = (
                <ListRow
                  ref={(el: HTMLAnchorElement | HTMLDivElement | null) => {
                    rows.current[i] = el
                  }}
                  variant="lane"
                  glyph={it.glyph ?? mark.glyph}
                  tone={it.glyph ? 'idle' : glyphTone(mark.tone)}
                  glyphLabel={sentence(ROW_NAMES[g])}
                  title={it.name}
                  titleAttr={it.key}
                  sub={agentSub(it)}
                  right={it.lane ? <><MrBadge lane={it.lane} /> {it.lane.gate && <GateBadge gate={it.lane.gate} />}</> : undefined}
                  age={since(it.at, now)}
                  selected={op ? isOpenNow(op) : i === clamped}
                  onSelect={() => setSel(i)}
                  href={op ? openHref(op) : it.request ? href.decisions() : undefined}
                />
              )
              return (
                <li key={it.key} className="board-row-wrap">
                  <div className="board-row" data-selected={i === clamped ? 'true' : undefined} onClick={() => setSel(i)}>
                    {row}
                  </div>
                  {(() => {
                    // The overflow menu stays shut until the lane is finished: an '…' under
                    // every row is not a control, only the finished mark and its close are.
                    const review = it.lane && it.lane.state === 'open' && !it.decision && !it.request
                      ? finishedLanes.get(it.lane.id)
                      : undefined
                    if (!review || !it.lane) return null
                    return (
                      <p className="row-actions lane-close-row">
                        <FinishedMark review={review} lastRun={it.run?.state} />
                        <Popover label="…">
                          <LaneCloseButton lane={it.lane} running={it.run?.state === 'running'} />
                        </Popover>
                      </p>
                    )
                  })()}
                  {it.run && runId !== undefined && (canDismiss(it) || (dismissed?.show && it.run.dismissed)) && (
                    <p className="row-actions">
                      {it.run.dismissed ? (
                        <button type="button" className="link-button" onClick={() => void dismiss(runId, true)}>
                          Undismiss run {runId}
                        </button>
                      ) : (
                        <button
                          type="button"
                          className="link-button"
                          title="Clear this run from the board. Its log and record stay in history."
                          onClick={() => void dismiss(runId)}
                        >
                          Dismiss run {runId}
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
      )
    })
  }

  return (
    <div className="board">
      <header className="page-head">
        <h1>Board</h1>
        <p className="page-sub">{summary || 'Nothing needs you right now.'}</p>
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
            const id = `project-${g.unassigned ? 'unassigned' : g.name}`
            return (
              <section key={g.name} className="board-project" aria-labelledby={id}>
                <GroupHeader
                  id={id}
                  variant="project"
                  label={g.title}
                  meta={summaryText(g.summary, (n) => usd(n) || '$0.00')}
                  spend={(
                    <button type="button" className="link-button" onClick={() => go(projectHref({ name: g.name, unassigned: g.unassigned }))}>
                      Open
                    </button>
                  )}
                />
                <div className="project-group-body">
                  {g.items.length === 0 ? <p className="project-group-empty faint">Nothing needs attention here.</p> : renderGroups(g.items as Row[])}
                </div>
              </section>
            )
          })
        : renderGroups(items)}
      {grouped && quiet.length > 0 && (
        <section className="quiet-projects" aria-label="Quiet projects">
          <button
            type="button"
            className="project-toggle"
            aria-expanded={quietOpen}
            onClick={() => setQuietOpen((o) => !o)}
          >
            <span className="caret" aria-hidden="true">{quietOpen ? '▾' : '▸'}</span>
            {quiet.length} quiet {quiet.length === 1 ? 'project' : 'projects'}
          </button>
          {quietOpen && (
            <ul className="quiet-list">
              {quiet.map((g) => {
                const to = projectHref({ name: g.name, unassigned: g.unassigned })
                return (
                  <li key={collapseKey(g)}>
                    <a href={to} onClick={(e) => { e.preventDefault(); go(to) }}>{g.title}</a>
                    <span className="faint">{summaryText(g.summary, (n) => usd(n) || '$0.00')}</span>
                  </li>
                )
              })}
            </ul>
          )}
        </section>
      )}
      <FinishedSection lanes={done} range={range} loading={closed.loading && closed.data === null} error={closed.error} offset={shown} now={now} selected={clamped} onSelect={setSel} rows={rows} />
    </div>
  )
}

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
    <section className="board-group group-finished" aria-labelledby="group-finished">
      <GroupHeader
        id="group-finished"
        variant="status"
        glyph="−"
        glyphTone="idle"
        label="Finished"
        count={lanes.length}
        meta={<RangeFilter range={range} href={href.board} />}
      />
      {error && <p className="group-note tone-bad">Could not load closed lanes: {error}</p>}
      {loading && !error && <p className="group-note faint">Loading…</p>}
      {!loading && !error && lanes.length === 0 && <p className="group-note faint">No lane closed {phrase}.</p>}
      {!loading && !error && lanes.length > 0 && (
        <ul className="board-rows">
          {lanes.map((l, n) => {
            const i = offset + n
            const end = ending(l)
            const finishedTone = glyphTone(end.tone)
            const rightTone: 'ok' | 'bad' | 'muted' = end.tone === 'ok' ? 'ok' : 'muted'
            return (
              <li key={l.id} className="board-row-wrap">
                <ListRow
                  ref={(el: HTMLAnchorElement | HTMLDivElement | null) => {
                    rows.current[i] = el
                  }}
                  variant="lane"
                  glyph="−"
                  tone={finishedTone}
                  glyphLabel="closed"
                  title={l.name}
                  sub={`${l.repo} · ${end.text} · ${l.runs} ${l.runs === 1 ? 'run' : 'runs'} · ${runCost(l) || usd(0)}`}
                  right={l.mr ? `!${l.mr}` : undefined}
                  rightTone={rightTone}
                  age={`${since(l.closed, now)} ago`}
                  selected={i === selected}
                  onSelect={() => onSelect(i)}
                  href={openHref({ kind: 'lane', id: l.id })}
                  titleAttr={end.title}
                />
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}
