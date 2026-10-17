import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { config } from '../api/client'
import type { LaneRecord, LaneReview, LaneView, RunView } from '../api/types'
import { FinishedMark, LaneCloseButton, useFinishedLanes } from '../components/LaneClose'
import { LaneApprove } from '../components/LaneApprove'
import { GateBadge } from '../components/GateBadge'
import { MrBadge } from '../components/MrBadge'
import { RunFailure } from '../components/RunFailure'
import { editorHref } from '../editorLink'
import { duration, runCost, shortSha, since } from '../format'
import { boardItems, GROUP_NAMES } from '../model/board'
import { ending } from '../model/history'
import { GROUP_MARKS } from '../model/groups'
import { href } from '../router'
import { glyphTone, StatusGlyph } from './StatusGlyph'
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

  const action = latest?.state === 'running' ? undefined
    : latest?.diagnosis ? 'failure'
    : defaultLaneDetailTab(lane, runs) === 'changes' ? 'approve'
    : lane.state === 'open' && review?.verdict === 'finished' ? 'close'
    : undefined

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
          review={review}
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

function LaneOverview({ lane, runs, latest, action, review, now, feedLast, known, record, editorUrl }: {
  lane: LaneView
  runs: RunView[]
  latest?: RunView
  action?: 'approve' | 'close' | 'failure'
  review?: LaneReview
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
  const decisions = snap.decisions.filter((d) => d.lane === lane.name && d.repo === lane.repo)
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

      {action === 'failure' && latest && (
        <div className="lane-detail-action">
          <RunFailure run={latest} onFixed={() => void live.refresh()} />
        </div>
      )}
      {action === 'approve' && lane.state === 'open' && (
        <div className="lane-detail-action">
          <LaneApprove laneId={lane.id} refreshKey={feedLast} />
        </div>
      )}
      {lane.state === 'open' && (
        <div className="lane-detail-action">
          {action === 'close' && review && <FinishedMark review={review} lastRun={latest?.state} detail />}
          <LaneCloseButton lane={lane} running={runs.some((r) => r.state === 'running')} />
        </div>
      )}
    </div>
  )
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
