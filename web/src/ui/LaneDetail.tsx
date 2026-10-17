import { useEffect, useMemo, useState } from 'react'
import { laneChanges } from '../api/client'
import type { LaneReview, LaneView, RunView } from '../api/types'
import { FinishedMark, LaneCloseButton, useFinishedLanes } from '../components/LaneClose'
import { LaneApprove } from '../components/LaneApprove'
import { GateBadge } from '../components/GateBadge'
import { MrBadge } from '../components/MrBadge'
import { RunFailure } from '../components/RunFailure'
import { editorHref } from '../editorLink'
import { duration, runCost, since } from '../format'
import { useLive } from '../state/context'
import { LaneArtifactsPanel } from '../artifacts'
import type { ConversationSource } from '../conversation/types'
import { Conversation } from '../conversation/Conversation'
import { laneSource } from '../conversation/api'
import { ChangesList, type RunRange } from './ChangesList'
import { FileBrowser } from './FileBrowser'
import { RunsTable } from './RunsTable'
import { Tabs } from './Tabs'
import { tabLabel, useLaneCounts } from '../inspector/laneCounts'

const TABS = ['overview', 'changes', 'artifacts', 'conversation', 'runs', 'files'] as const
export type LaneDetailTab = (typeof TABS)[number]

/** A tab name from an `open=lane:N/tab` address; anything unknown is the overview. */
export function laneDetailTab(tab: string | undefined): LaneDetailTab {
  return (TABS as readonly string[]).includes(tab ?? '') ? (tab as LaneDetailTab) : 'overview'
}

function needsReview(lane: LaneView): boolean {
  return lane.state === 'open' && !!lane.mr && lane.mr_state === 'open' && lane.pipeline_status === 'passed'
}

/** The tab a lane opens on: changes when it is waiting for a merge review, overview otherwise. */
export function defaultLaneDetailTab(lane: LaneView): LaneDetailTab {
  return needsReview(lane) ? 'changes' : 'overview'
}

/**
 * The detail of one lane, as the side panel on the Board or the "Open as page" view. The tab
 * is owned by the caller and usually lives in the URL, so Back and links bring it back.
 */
export function LaneDetail({ lane, runs, now, tab, onTab, feedLast, conversation, full }: {
  lane: LaneView
  runs: RunView[]
  now: number
  tab: LaneDetailTab
  onTab: (t: LaneDetailTab) => void
  feedLast: number
  conversation?: (laneId: number) => ConversationSource
  full?: boolean
}) {
  const counts = useLaneCounts(lane.id, feedLast)
  const latest = runs[0]
  const source = useMemo(() => (conversation ?? laneSource)(lane.id), [conversation, lane.id])
  const [editorUrl] = useState<string | null>(null)
  const finished = useFinishedLanes(lane.state === 'open' ? [lane] : [], feedLast)
  const review = finished.get(lane.id)

  const action = latest?.state === 'running' ? undefined
    : latest?.diagnosis ? 'failure'
    : needsReview(lane) ? 'approve'
    : lane.state === 'open' && review?.verdict === 'finished' ? 'close'
    : undefined

  const runRange: RunRange | undefined = useMemo(() => {
    if (!latest) return undefined
    const max = Math.max(...runs.map((r) => r.id), latest.id)
    return { id: latest.id, latest: latest.id === max, outside: latest.outside }
  }, [latest, runs])

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
        />
      ),
    },
    {
      id: 'changes',
      label: tabLabel('Changes', counts.changes),
      content: (
        <>
          {action === 'approve' && lane.state === 'open' && (
            <div className="lane-detail-action">
              <LaneApprove laneId={lane.id} refreshKey={feedLast} />
            </div>
          )}
          <LaneChanges lane={lane} run={runRange} editorUrl={editorUrl} feedLast={feedLast} />
        </>
      ),
    },
    { id: 'artifacts', label: tabLabel('Artifacts', counts.artifacts), content: <LaneArtifactsPanel laneId={lane.id} refreshKey={feedLast} /> },
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
    { id: 'files', label: 'Files', content: <FileBrowser laneId={lane.id} /> },
  ]

  return (
    <div className="lane-detail" data-component="LaneDetail" data-layout={full ? 'page' : 'panel'}>
      {full && (
        <header className="lane-detail-head">
          <h1>{lane.name}</h1>
        </header>
      )}
      <Tabs items={items} value={tab} onChange={(t) => onTab(t as LaneDetailTab)} label="Lane views" />
    </div>
  )
}

function LaneOverview({ lane, latest, action, review, now, feedLast }: {
  lane: LaneView
  runs: RunView[]
  latest?: RunView
  action?: 'approve' | 'close' | 'failure'
  review?: LaneReview
  now: number
  feedLast: number
}) {
  const live = useLive()
  const link = editorHref(null, lane.worktree)
  return (
    <div className="lane-detail-overview">
      <dl className="sh-dl">
        <dt>Repo</dt>
        <dd>{lane.repo}</dd>
        <dt>Branch</dt>
        <dd>
          <code>{lane.branch}</code>
          {lane.base && <> onto <code>{lane.base}</code></>}
        </dd>
        <dt>Opened</dt>
        <dd>{since(lane.created, now)} ago</dd>
        <dt>Merge request</dt>
        <dd>{lane.mr ? <><MrBadge lane={lane} /> <span className="faint">{lane.mr_state}</span></> : <span className="faint">None yet</span>}</dd>
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
        <dt>Worktree</dt>
        <dd>
          <code className="path">{lane.worktree}</code>
          {link && <> <a href={link} title="Open in editor">open in editor</a></>}
        </dd>
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
          <RunFailure run={latest} onFixed={() => void live.refresh().then(() => {})} />
        </div>
      )}
      {action === 'approve' && lane.state === 'open' && (
        <div className="lane-detail-action">
          <LaneApprove laneId={lane.id} refreshKey={feedLast} />
        </div>
      )}
      {action === 'close' && review && lane.state === 'open' && (
        <div className="lane-detail-action">
          <FinishedMark review={review} lastRun={latest?.state} detail />
          <LaneCloseButton lane={lane} running={latest?.state === 'running'} />
        </div>
      )}
      {action === undefined && lane.state === 'open' && (
        <div className="lane-detail-action">
          <LaneCloseButton lane={lane} running={latest?.state === 'running'} />
        </div>
      )}
    </div>
  )
}

function LaneChanges({ lane, run, editorUrl, feedLast }: {
  lane: LaneView
  run?: RunRange
  editorUrl: string | null
  feedLast: number
}) {
  const [read, setRead] = useState<import('../api/types').LaneDiff | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(true)
  useEffect(() => {
    let current = true
    setBusy(true)
    laneChanges.diff(lane.id, undefined, run?.id).then(
      (d) => {
        if (!current) return
        setRead(d)
        setError(null)
      },
      (e: unknown) => current && setError(e instanceof Error ? e.message : String(e)),
    ).finally(() => current && setBusy(false))
    return () => {
      current = false
    }
  }, [lane.id, run?.id, feedLast])
  if (busy && !read) return <p className="faint">Reading…</p>
  if (error) return <p className="tone-bad">Could not read the lane's changes: {error}</p>
  if (!read) return null
  return (
    <ChangesList
      read={read}
      worktree={lane.worktree}
      editorUrl={editorUrl}
      run={run}
      variant="diff"
    />
  )
}
