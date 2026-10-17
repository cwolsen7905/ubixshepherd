import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { config } from '../api/client'
import type { ReactNode } from 'react'
import type { DecisionView, LaneRecord, LaneReview, LaneView, NextItem, RequestView, RunView, WaitingItem } from '../api/types'
import { FinishedMark, LaneCloseButton, useFinishedLanes } from '../components/LaneClose'
import { LaneApprove } from '../components/LaneApprove'
import { RunFailure } from '../components/RunFailure'
import { clockTime, since } from '../format'
import { boardItems, endedBadly, GROUP_NAMES } from '../model/board'
import { ending } from '../model/history'
import { aboutLane } from '../model/feed'
import { MARKS } from '../model/marks'
import { GROUP_MARKS } from '../model/groups'
import { go, href, withOpen } from '../router'
import { PageHeader, PageTabs, pageTabIds, usePageTabBase, type PageChrome } from './PageHeader'
import { ApproveAction, useLaneApproval, type LaneApprovalState } from './LaneDetailApprove'
import { CloseLaneAction } from './LaneDetailClose'
import { LaneChangesTab } from './LaneDetailChanges'
import { LaneFacts } from './LaneDetailFacts'
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
export function LaneDetail({ lane, runs, now, tab, onTab, feedLast, conversation, full, back, chrome, known = true, record }: {
  lane: LaneView
  runs: RunView[]
  now: number
  tab: LaneDetailTab
  onTab: (t: LaneDetailTab) => void
  feedLast: number
  conversation?: (laneId: number) => ConversationSource
  full?: boolean
  /** The page's back link, drawn in its header. */
  back?: ReactNode
  /** Where the page's header takes the person: the board by default. */
  chrome?: Partial<PageChrome>
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

  const approval = useLaneApproval(lane.id, feedLast, !!full && known && lane.state === 'open')
  const tabBase = usePageTabBase()

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
          page={!!full}
          approval={approval}
        />
      ),
    },
    {
      id: 'changes',
      label: tabLabel('Changes', counts.changes),
      content: full && known ? (
        <LaneChangesTab lane={lane} runs={runs} latest={latest} now={now} editorUrl={editorUrl} refreshKey={feedLast} approval={approval} />
      ) : (
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

  const current = items.find((t) => t.id === tab) ?? items[0]
  const here = full
    ? {
        back,
        beside: lane.state === 'open' ? { label: 'Open beside the board', onClick: () => go(withOpen(href.board(), { kind: 'lane', id: lane.id, tab })) } : undefined,
        onClose: () => go(href.board()),
        ...chrome,
      }
    : undefined
  const tabEls = full ? (
    <PageTabs tabs={items.map(({ id, label }) => ({ id, label }))} value={current?.id ?? tab} onChange={(t) => onTab(t as LaneDetailTab)} label="Lane views" base={tabBase} />
  ) : null

  return (
    <div ref={root} className="lane-detail" data-component="LaneDetail" data-layout={full ? 'page' : 'panel'}>
      {full && here ? (
        <>
          <LaneHeader lane={lane} runs={runs} latest={latest} action={action} approval={approval} tab={tab} onTab={onTab} now={now} chrome={here} tabs={tabEls} />
          {current && (
            <div role="tabpanel" id={pageTabIds(tabBase, current.id).panel} aria-labelledby={pageTabIds(tabBase, current.id).tab} className="sh-tabpanel">
              {current.content}
            </div>
          )}
        </>
      ) : (
        <Tabs items={items} value={tab} onChange={(t) => onTab(t as LaneDetailTab)} label="Lane views" />
      )}
    </div>
  )
}

/**
 * The page's header, from the shared PageHeader: the lane's title and meta line, the state's
 * primary action, a secondary one, and Close lane… on the right. The tabs sit under it.
 */
function LaneHeader({ lane, runs, latest, action, approval, tab, onTab, now, chrome, tabs }: {
  lane: LaneView
  runs: RunView[]
  latest?: RunView
  action?: LanePrimary
  approval: LaneApprovalState
  tab: LaneDetailTab
  onTab: (t: LaneDetailTab) => void
  now: number
  chrome: PageChrome
  tabs: ReactNode
}) {
  const snap = useSnapshot()
  // The lane's open decisions and requests count: the board files them as 'needs you', and the word must agree.
  const group = useMemo(() => {
    const decisions = snap.decisions.filter((d) => d.state === 'open' && d.lane === lane.name && d.repo === lane.repo)
    const requests = snap.requests.filter((r) => r.state === 'needs_routing' && r.from_lane === lane.name && r.repo === lane.repo)
    const items = boardItems({ ...snap, lanes: [lane], decisions, requests })
    return (items.find((it) => it.decision || it.request) ?? items[0])?.group
  }, [snap, lane])
  const state = group ? GROUP_NAMES[group].toLowerCase() : lane.state
  const secondary = latest && action?.kind !== 'open' && action?.kind !== 'failure' && action?.kind !== 'follow'
  // Approve is the header's own once the forge says the merge request can be approved: the primary of a lane
  // to review, and on the Changes tab whatever else the lane's state asks first.
  const approve = approval.canApprove && (action?.kind === 'approve' || tab === 'changes')
  const running = runs.some((r) => r.state === 'running')
  return (
    <PageHeader
      {...chrome}
      handleEscape={chrome.escape !== 'beside'}
      glyph={group && <StatusGlyph glyph={GROUP_MARKS[group].glyph} tone={glyphTone(GROUP_MARKS[group].tone)} label={GROUP_NAMES[group]} />}
      title={lane.name}
      meta={
        <>
          <span>{lane.repo}</span>
          <span>
            <code>{lane.branch}</code>
            {lane.base && <> onto <code>{lane.base}</code></>}
          </span>
          <span>{lane.state === 'closed' ? `closed ${since(lane.closed, now)} ago` : `opened ${since(lane.created, now)} ago`}</span>
          <span>{state}</span>
        </>
      }
      actions={
        lane.state === 'open' && (
          <>
            {action && action.kind !== 'approve' && <PrimaryButton lane={lane} action={action} />}
            {action?.kind === 'approve' && !approve && tab !== 'changes' && <PrimaryButton lane={lane} action={action} />}
            {/* One lantern primary per view: the state's own, else Approve. */}
            {approve && <ApproveAction approval={approval} secondary={!!action && action.kind !== 'approve'} />}
            {approve && tab !== 'changes' && <Button variant="ghost" onClick={() => onTab('changes')}>Review changes</Button>}
            {secondary && latest && <Button variant="ghost" onClick={() => go(href.log(latest.id))}>Open run</Button>}
          </>
        )
      }
      danger={
        lane.state === 'open' && (
          <>
            {action?.kind === 'close' && <FinishedMark review={action.review} lastRun={latest?.state} detail />}
            <CloseLaneAction lane={lane} running={running} />
          </>
        )
      }
      tabs={tabs}
    />
  )
}

/** The lane's own events from the feed, newest last, as the page's timeline. */
function LaneTimeline({ lane, now }: { lane: LaneView; now: number }) {
  const snap = useSnapshot()
  const events = useMemo(() => {
    const refs = {
      runs: snap.runs,
      decisionRuns: new Map(snap.decisions.map((d) => [d.id, d.run_id] as const)),
      requests: snap.requests,
    }
    return snap.feed.filter((f) => aboutLane(f, lane, refs)).slice(-20)
  }, [snap.feed, snap.runs, snap.decisions, snap.requests, lane])
  return (
    <section aria-labelledby="lane-timeline-h">
      <h2 id="lane-timeline-h" className="lane-section-h">Timeline</h2>
      {events.length === 0 ? (
        <p className="faint">Nothing yet in this lane.</p>
      ) : (
        <ol className="lane-timeline">
          {events.map((e) => {
            const mark = MARKS[e.event]
            return (
              <li key={e.id}>
                <StatusGlyph glyph={mark.glyph} tone={glyphTone(mark.tone)} label={mark.label} />
                <time dateTime={e.created} title={e.created}>{clockTime(e.created, now)}</time>
                <span>{e.text}</span>
              </li>
            )
          })}
        </ol>
      )}
    </section>
  )
}

function LaneOverview({ lane, runs, latest, action, now, feedLast, known, record, editorUrl, page, approval }: {
  lane: LaneView
  runs: RunView[]
  latest?: RunView
  action?: LanePrimary
  now: number
  feedLast: number
  known: boolean
  record?: LaneRecord
  editorUrl: string | null
  page: boolean
  approval: LaneApprovalState
}) {
  const live = useLive()
  const snap = useSnapshot()
  const group = useMemo(() => boardItems({ ...snap, lanes: [lane], decisions: [], requests: [] })[0]?.group, [snap, lane])
  const decisions = snap.decisions.filter((d) => d.state === 'open' && d.lane === lane.name && d.repo === lane.repo)
  const facts = <LaneFacts lane={lane} runs={runs} latest={latest} now={now} editorUrl={editorUrl} page={page} approval={page ? approval : undefined} />
  const failure = lane.state === 'open' && (action?.kind === 'open' || action?.kind === 'failure') && (
    <div className="lane-detail-action">
      <RunFailure run={action.run} onFixed={() => void live.refresh()} />
    </div>
  )
  if (page) {
    return (
      <div className="lane-detail-overview lane-overview-page">
        <div className="lane-overview-main">
          {record && lane.state === 'closed' && <p className="lane-detail-status"><span title={ending(record).title}>{ending(record).text}</span></p>}
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
          {failure}
          <h2 className="lane-section-h">Runs</h2>
          <RunsTable runs={runs} variant="full" />
          <LaneTimeline lane={lane} now={now} />
        </div>
        <aside className="lane-facts-rail" aria-label="Lane facts">
          {facts}
        </aside>
      </div>
    )
  }

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

      {facts}

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
