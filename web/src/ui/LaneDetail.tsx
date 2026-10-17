import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { config } from '../api/client'
import type { DecisionView, LaneRecord, LaneReview, LaneView, NextItem, RequestView, RunView, WaitingItem } from '../api/types'
import { FinishedMark, LaneCloseButton, useFinishedLanes } from '../components/LaneClose'
import { LaneApprove } from '../components/LaneApprove'
import { GateBadge } from '../components/GateBadge'
import { MrBadge } from '../components/MrBadge'
import { RunFailure } from '../components/RunFailure'
import { editorHref } from '../editorLink'
import { duration, runCost, shortSha, since } from '../format'
import { boardItems, endedBadly, GROUP_NAMES } from '../model/board'
import { ending } from '../model/history'
import { GROUP_MARKS } from '../model/groups'
import { go, href, withOpen } from '../router'
import { glyphTone, StatusGlyph } from './StatusGlyph'
import { Button } from './Button'
import { useLive, useSnapshot } from '../state/context'
import { LaneArtifactsPanel } from '../artifacts'
import type { ConversationSource } from '../conversation/types'
import { Conversation } from '../conversation/Conversation'
import { laneSource } from '../conversation/api'
import { Changes } from '../components/Changes'
import { FileBrowser } from './FileBrowser'
import { RunsTable } from './RunsTable'
import { Tabs } from './Tabs'
import { tabLabel, useLaneCounts } from '../inspector/laneCounts'
import { rememberedScroll, rememberScroll, rememberTab, scroller } from './LaneDetailMemory'

const NO_WORKTREE = 'This lane is closed or not listed by the daemon, so there is no worktree to read.'

const TABS = ['overview', 'changes', 'artifacts', 'conversation', 'runs', 'files'] as const
export type LaneDetailTab = (typeof TABS)[number]

/** A tab name from an `open=lane:N/tab` address; anything unknown is the overview. */
export function laneDetailTab(tab: string | undefined): LaneDetailTab {
  return (TABS as readonly string[]).includes(tab ?? '') ? (tab as LaneDetailTab) : 'overview'
}

/** The editor link template (web.editor_url); null until read, or when it cannot be read. */
function useEditorUrl(): string | null {
  const [url, setUrl] = useState<string | null>(null)
  useEffect(() => {
    let current = true
    config.get().then(
      (doc) => {
        const web = (doc.effective?.web ?? {}) as { editor_url?: unknown }
        if (current && typeof web.editor_url === 'string') setUrl(web.editor_url)
      },
      () => {},
    )
    return () => {
      current = false
    }
  }, [])
  return url
}

/**
 * The Overview's primary action: the one thing the lane's state asks of the person, so the
 * side panel's first row acts instead of only describing. The waiting lists decide: an open
 * decision of this lane, then a request to route, then the runs, then the next approved
 * item of the repo. A target that does not exist is no button at all.
 */
export type LanePrimary =
  | { kind: 'answer'; decision: DecisionView }
  | { kind: 'route'; request: RequestView }
  | { kind: 'open'; run: RunView }
  | { kind: 'follow'; run: RunView }
  | { kind: 'next'; item: NextItem }
  | { kind: 'approve' }
  | { kind: 'close'; review: LaneReview }
  | { kind: 'failure'; run: RunView }

export function laneStateAction({ lane, runs, latest, review, decisions, requests, next, waiting }: {
  lane: LaneView
  runs: RunView[]
  latest?: RunView
  review?: LaneReview
  decisions: DecisionView[]
  requests: RequestView[]
  next: NextItem[]
  waiting: WaitingItem[]
}): LanePrimary | undefined {
  if (lane.state !== 'open') return undefined
  // The daemon's attention list names what waits; the lane's own lists give the target.
  const waits = new Set(waiting.flatMap((w) => {
    if (w.action === 'answer' && w.decision_id) return [`decision:${w.decision_id}`]
    if (w.action === 'route' && w.request_id) return [`request:${w.request_id}`]
    return []
  }))
  const decision = decisions.find((d) => waits.has(`decision:${d.id}`)) ?? decisions[0]
  if (decision) return { kind: 'answer', decision }
  const request = requests.find((r) => waits.has(`request:${r.id}`)) ?? requests[0]
  if (request) return { kind: 'route', request }
  const running = runs.find((r) => r.state === 'running') ?? (latest?.state === 'running' ? latest : undefined)
  if (running) return { kind: 'follow', run: running }
  // Only the latest run: an older failure a newer run has passed is history, not the state.
  const failed = latest && endedBadly(latest) && !latest.dismissed ? latest : undefined
  if (failed) return { kind: 'open', run: failed }
  if (latest?.diagnosis && !latest.dismissed) return { kind: 'failure', run: latest }
  if (defaultLaneDetailTab(lane, runs) === 'changes') return { kind: 'approve' }
  if (review?.verdict === 'finished') return { kind: 'close', review }
  // Only an item no lane has taken: one assigned to a lane is that lane's, not an offer.
  const free = next.filter((n) => !n.lane_id)
  const item = free.find((n) => waiting.some((w) => w.action === 'review' && w.next_id === n.id)) ?? free[0]
  if (item) return { kind: 'next', item }
  return undefined
}

/** The tab a lane opens on: changes when it is in the board's "to review" group, overview otherwise. */
export function defaultLaneDetailTab(lane: LaneView, runs: RunView[] = []): LaneDetailTab {
  const seen = { until: '', keys: new Set<string>() }
  const group = boardItems({ lanes: [lane], runs, decisions: [], requests: [], seen })[0]?.group
  return group === 'review' ? 'changes' : 'overview'
}

/**
 * The detail of one lane, as the side panel on the Board or the "Open as page" view. The tab
 * is owned by the caller and usually lives in the URL, so Back and links bring it back.
 */
export function LaneDetail({ lane, runs, now, tab, onTab, feedLast, conversation, full, known = true, record }: {
  lane: LaneView
  runs: RunView[]
  now: number
  tab: LaneDetailTab
  onTab: (t: LaneDetailTab) => void
  feedLast: number
  conversation?: (laneId: number) => ConversationSource
  full?: boolean
  /** False for a lane the daemon no longer lists: there is no worktree to read. */
  known?: boolean
  /** The history's record of a closed lane, for how it ended. */
  record?: LaneRecord
}) {
  const counts = useLaneCounts(lane.id, feedLast)
  const root = useRef<HTMLDivElement>(null)
  const latest = runs[0]
  const source = useMemo(() => (conversation ?? laneSource)(lane.id), [conversation, lane.id])
  const editorUrl = useEditorUrl()
  const finished = useFinishedLanes(lane.state === 'open' ? [lane] : [], feedLast)
  const review = finished.get(lane.id)
  const snap = useSnapshot()
  const waiting = snap.waiting?.items ?? []

  const action = laneStateAction({
    lane,
    runs,
    latest,
    review,
    // The state's own: an open decision or a request to route first, then the runs, then
    // the next approved item waiting for a lane of this repo.
    decisions: snap.decisions.filter((d) => d.state === 'open' && d.lane === lane.name && d.repo === lane.repo),
    requests: snap.requests.filter((r) => r.state === 'needs_routing' && r.from_lane === lane.name && r.repo === lane.repo),
    next: snap.next.filter((n) => n.state === 'approved' && n.stored_state === 'approved' && (!n.repo || n.repo === lane.repo) && !n.lane_id),
    waiting,
  })

  const items = [
    {
      id: 'overview',
      label: 'Overview',
      content: (
        <LaneOverview
          lane={lane}
          runs={runs}
          latest={latest}
          action={action}
          now={now}
          feedLast={feedLast}
          known={known}
          record={record}
          editorUrl={editorUrl}
        />
      ),
    },
    {
      id: 'changes',
      label: tabLabel('Changes', counts.changes),
      content: (
        <>
          {known && lane.state === 'open' && (
            <div className="lane-detail-action">
              <LaneApprove laneId={lane.id} refreshKey={feedLast} />
            </div>
          )}
          {known ? <Changes laneId={lane.id} worktree={lane.worktree} editorUrl={editorUrl} refreshKey={feedLast} now={now} /> : <p className="lane-note faint">{NO_WORKTREE}</p>}
        </>
      ),
    },
    { id: 'artifacts', label: tabLabel('Artifacts', counts.artifacts), content: known && lane.state === 'open' ? <LaneArtifactsPanel laneId={lane.id} refreshKey={feedLast} /> : <p className="lane-note faint">{NO_WORKTREE}</p> },
    {
      id: 'conversation',
      label: 'Conversation',
      content: (
        // TODO(step-2): replace with Thread when it lands.
        <div className="lane-detail-placeholder">
          <Conversation source={source} refreshKey={feedLast} now={now} />
        </div>
      ),
    },
    { id: 'runs', label: tabLabel('Runs', runs.length), content: <RunsTable runs={runs} variant="full" /> },
    { id: 'files', label: 'Files', content: known && lane.state === 'open' ? <FileBrowser laneId={lane.id} layout="tree-viewer" /> : <p className="lane-note faint">{NO_WORKTREE}</p> },
  ]

  // The tab and how far it was scrolled outlive the panel: the page opens where the panel was.
  useEffect(() => rememberTab(lane.id, tab), [lane.id, tab])
  useLayoutEffect(() => {
    const box = scroller(root.current)
    const target: EventTarget = box === document.scrollingElement || box === document.documentElement ? window : box
    box.scrollTop = rememberedScroll(lane.id, tab)
    const save = () => rememberScroll(lane.id, tab, box.scrollTop)
    target.addEventListener('scroll', save, { passive: true })
    return () => target.removeEventListener('scroll', save)
  }, [lane.id, tab])

  return (
    <div ref={root} className="lane-detail" data-component="LaneDetail" data-layout={full ? 'page' : 'panel'}>
      {full && (
        <header className="lane-detail-head">
          <h1>{lane.name}</h1>
        </header>
      )}
      <Tabs items={items} value={tab} onChange={(t) => onTab(t as LaneDetailTab)} label="Lane views" />
    </div>
  )
}

function LaneOverview({ lane, runs, latest, action, now, feedLast, known, record, editorUrl }: {
  lane: LaneView
  runs: RunView[]
  latest?: RunView
  action?: LanePrimary
  now: number
  feedLast: number
  known: boolean
  record?: LaneRecord
  editorUrl: string | null
}) {
  const live = useLive()
  const snap = useSnapshot()
  const link = editorHref(editorUrl, lane.worktree)
  const group = useMemo(() => boardItems({ ...snap, lanes: [lane], decisions: [], requests: [] })[0]?.group, [snap, lane])
  const decisions = snap.decisions.filter((d) => d.state === 'open' && d.lane === lane.name && d.repo === lane.repo)
  const pipelineUrl = pipelineLink(lane)
  const commits = runs.filter((r) => r.commits > 0)
  return (
    <div className="lane-detail-overview">
      <p className="lane-detail-status">
        {group && <StatusGlyph glyph={GROUP_MARKS[group].glyph} tone={glyphTone(GROUP_MARKS[group].tone)} label={GROUP_NAMES[group]} />}
        <span>{group ? GROUP_NAMES[group] : lane.state}</span>
        {latest && <span className="faint">{latest.agent}</span>}
        <span className="faint">{lane.state === 'closed' ? `closed ${since(lane.closed, now)} ago` : `opened ${since(lane.created, now)} ago`}</span>
        {record && lane.state === 'closed' && <span title={ending(record).title}>{ending(record).text}</span>}
      </p>

      {!known && (
        <p className="lane-note faint">
          This lane is closed or not listed by the daemon, so its scope and merge request are not shown; its runs are.
        </p>
      )}

      {latest?.state === 'running' && latest.progress && (
        <p className="lane-progress" aria-live="polite">
          <StatusGlyph glyph="▸" tone="live" label="running" /> Run {latest.id}: {latest.progress.line}
        </p>
      )}

      {decisions.length > 0 && (
        <p className="lane-decisions">
          <StatusGlyph glyph="?" tone="warn" label="decision waiting" />
          <span>
            {decisions.length === 1 ? 'A decision waits on you: ' : `${decisions.length} decisions wait on you: `}
            {decisions.map((d, i) => (
              <span key={d.id}>
                {i > 0 && ', '}
                <a href={href.decisions(d.id)}>{d.question.length > 80 ? d.question.slice(0, 79) + '…' : d.question}</a>
              </span>
            ))}
          </span>
        </p>
      )}

      <dl className="sh-dl">
        <dt>Repo</dt>
        <dd>{lane.repo}</dd>
        <dt>Branch</dt>
        <dd>
          <code>{lane.branch}</code>
          {lane.base && <> onto <code>{lane.base}</code></>}
        </dd>
        <dt>Merge request</dt>
        <dd>
          {lane.mr ? (
            <>
              <MrBadge lane={lane} /> <span className="faint">{lane.mr_state}</span>
              {lane.pipeline ? (
                <>
                  {' '}
                  {pipelineUrl ? <a href={pipelineUrl} target="_blank" rel="noreferrer">pipeline {lane.pipeline}</a> : `pipeline ${lane.pipeline}`}{' '}
                  <span className="faint">{lane.pipeline_status}</span>
                </>
              ) : (
                <span className="faint"> no pipeline seen</span>
              )}
            </>
          ) : (
            <span className="faint">None yet</span>
          )}
        </dd>
        {lane.gate && (
          <>
            <dt>Gate</dt>
            <dd><GateBadge gate={lane.gate} now={now} /></dd>
          </>
        )}
        <dt>Scope</dt>
        <dd>
          <ul className="scope">
            {lane.scope.map((s) => (
              <li key={s}><code>{s}</code></li>
            ))}
          </ul>
        </dd>
        <dt>Commits</dt>
        <dd>
          {commits.length > 0 ? (
            <ul className="commits">
              {commits.map((r) => (
                <li key={r.id}>
                  <code>{shortSha(r.start_sha)}..{shortSha(r.end_sha)}</code> {r.commits} by run {r.id}
                </li>
              ))}
            </ul>
          ) : (
            <span className="faint">None from Shepherd's runs</span>
          )}
        </dd>
        <dt>Worktree</dt>
        <dd>
          <code className="path">{lane.worktree}</code>
          {link && <> <a href={link} title="Open in editor">open in editor</a></>}
        </dd>
        <dt>Opened by</dt>
        <dd>{origin(lane)}</dd>
        {latest && (
          <>
            <dt>Latest run</dt>
            <dd>
              Run {latest.id} · {latest.agent}
              {' · '}
              {latest.state}
              {latest.ended && <> · {duration(latest.started, latest.ended, now)}</>}
              {runCost(latest) && <> · {runCost(latest)}</>}
            </dd>
          </>
        )}
      </dl>

      {lane.state === 'open' && (
        <>
          <LanePrimaryAction lane={lane} runs={runs} latest={latest} action={action} />
          {(action?.kind === 'open' || action?.kind === 'failure') && (
            <div className="lane-detail-action">
              <RunFailure run={action.run} onFixed={() => void live.refresh()} />
            </div>
          )}
          {action?.kind === 'approve' && (
            <div className="lane-detail-action">
              <LaneApprove laneId={lane.id} refreshKey={feedLast} />
            </div>
          )}
        </>
      )}
    </div>
  )
}

/**
 * The Overview's action row: the state's primary action on the left, a ghost "Open as page"
 * beside it, and Close lane at the right. A missing target is no primary at all; the row
 * then shows only what applies.
 */
function LanePrimaryAction({ lane, runs, latest, action }: { lane: LaneView; runs: RunView[]; latest?: RunView; action?: LanePrimary }) {
  if (lane.state !== 'open') return null
  const primary = action && <PrimaryButton lane={lane} action={action} />
  return (
    <div className="lane-detail-action lane-detail-primary">
      {primary}
      <Button variant="ghost" onClick={() => go(href.lane(lane.id))}>
        Open as page
      </Button>
      <span className="lane-detail-close">
        {action?.kind === 'close' && <FinishedMark review={action.review} lastRun={latest?.state} detail />}
        <LaneCloseButton lane={lane} running={runs.some((r) => r.state === 'running')} />
      </span>
    </div>
  )
}

 /** The primary button for the lane's state, in the brief's words and tones. */
function PrimaryButton({ lane, action }: { lane: LaneView; action: LanePrimary }) {
  switch (action.kind) {
    case 'answer':
      // Read in the main body, with the topic thread beside it: the swap layout's own route.
      return <Button variant="attention" onClick={() => go(withOpen(href.chat(), { kind: 'decision', id: action.decision.id }))}>Answer decision</Button>
    case 'route':
      return <Button variant="attention" onClick={() => go(href.decisions())}>Route request</Button>
    case 'open':
      return <Button variant="primary" onClick={() => go(href.log(action.run.id))}>Open run</Button>
    case 'follow':
      // The live log in the side panel, so the lane stays in view beside it.
      return <Button variant="primary" onClick={() => go(withOpen(window.location.hash, { kind: 'run', id: action.run.id, tab: 'log' }))}>Follow run</Button>
    case 'next':
      return <Button variant="primary" onClick={() => go(href.next(action.item.id))}>Open next up</Button>
    case 'approve':
      return <Button variant="primary" onClick={() => go(withOpen(window.location.hash, { kind: 'lane', id: lane.id, tab: 'changes' }))}>Open changes</Button>
    case 'failure':
      return <Button variant="primary" onClick={() => go(href.log(action.run.id))}>Open run</Button>
    case 'close':
      // Finished already shows the mark beside the close below; no separate primary.
      return null
  }
}

/** The daemon gives a pipeline's id but not its URL; on GitLab it sits beside the MR's. */
function pipelineLink(lane: LaneView): string | null {
  if (!lane.mr_url || !lane.pipeline) return null
  const m = /^(.*)\/-\/merge_requests\/\d+$/.exec(lane.mr_url)
  return m ? `${m[1]}/-/pipelines/${lane.pipeline}` : null
}

function origin(l: LaneView): string {
  const o = l.origin
  if (!o.via) return 'unknown'
  const parts = [o.agent, o.run ? `run ${o.run}` : '', o.detail].filter(Boolean)
  return parts.length ? `${o.via} (${parts.join(', ')})` : o.via
}
