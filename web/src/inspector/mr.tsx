import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ApiError, laneApprove, laneChanges } from '../api/client'
import type { LaneApproval, LaneDiff, LaneView } from '../api/types'
import { LaneArtifactsPanel } from '../artifacts'
import { showWaiting } from '../chat/strip'
import { Changes } from '../components/Changes'
import { laneSource } from '../conversation/api'
import { Conversation } from '../conversation/Conversation'
import { since, shortSha } from '../format'
import { useNow } from '../hooks'
import { draftKey, prefillDraft } from '../model/chatDraft'
import { deskWorkspace } from '../model/next'
import { neighbour, waitingKey } from '../model/waitingStep'
import { inboxOpen } from '../pages/Inbox'
import { go, href, withOpen, type Open } from '../router'
import { useLive, useSnapshot } from '../state/context'
import { Button, EmptyState, Kbd, StatusGlyph, Tabs } from '../ui'
import { PageHeader, PageTabs, pageTabIds, usePageTabBase, type PageChrome } from '../ui/PageHeader'
import '../pages/Inbox.css'
import { LaneRuns } from './lane'
import { tabLabel, useLaneCounts } from './laneCounts'

export const MR_TABS = ['approval', 'changes', 'artifacts', 'conversation', 'runs'] as const
export type MrTab = (typeof MR_TABS)[number]

/** The tab in an `open=mr:4/changes` address; anything else is the approval. */
export function mrTab(tab: string | undefined): MrTab {
  return (MR_TABS as readonly string[]).includes(tab ?? '') ? (tab as MrTab) : 'approval'
}

/** The least time between two reads the feed triggers: a read asks the forge several things and runs git. */
const MIN_REREAD = 10_000

const PIPELINE_WORDS: Record<string, string> = { success: 'passed', failed: 'failed', canceled: 'was canceled', running: 'is running', pending: 'is pending' }

/** The words the person reads before approving: pipeline, the exact head, and what changed. */
function approvalSentence(a: LaneApproval, lane: LaneView, now: number): string {
  const sha = shortSha(a.sha)
  const words = a.pipeline_status ? (PIPELINE_WORDS[a.pipeline_status] ?? a.pipeline_status) : ''
  const when = lane.pipeline_finished ? ` ${since(lane.pipeline_finished, now)} ago` : ''
  const wait = a.require_approval === 'manual' ? ' and waits at the require-approval job' : ''
  const pipe = a.pipeline ? `Pipeline ${a.pipeline}${words ? ` ${words}` : ''}${when}${wait}. ` : ''
  return `${pipe}You are approving head ${sha}; a newer push asks again.`
}

/**
 * The Approval tab: the question, what the pipeline said, the commits, whether anything has
 * reviewed it, and the person's own Approve. The daemon refuses the approval to every token but
 * a browser session, and refuses a head that moved since this was read. Approving removes the
 * item from the Inbox and opens the next one.
 */
function MrApproval({ lane, refreshKey, now, onReview }: { lane: LaneView; refreshKey: number; now: number; onReview: () => void }) {
  const snap = useSnapshot()
  const live = useLive()
  const [view, setView] = useState<LaneApproval | null>(null)
  const [diff, setDiff] = useState<LaneDiff | null>(null)
  const [readError, setReadError] = useState(false)
  const [busy, setBusy] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [approved, setApproved] = useState(false)
  const [failure, setFailure] = useState<string | null>(null)
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  const last = useRef(0)
  const read = useCallback(async () => {
    last.current = Date.now()
    laneChanges.diff(lane.id).then(
      (d) => mounted.current && setDiff(d),
      () => undefined,
    )
    try {
      const v = await laneApprove.read(lane.id)
      if (!mounted.current) return
      setView(v)
      setReadError(false)
    } catch {
      if (mounted.current) setReadError(true)
    }
  }, [lane.id])
  useEffect(() => {
    // A feed change inside the window reads once when it ends, so the last state is not lost.
    const wait = last.current === 0 ? 0 : MIN_REREAD - (Date.now() - last.current)
    if (wait <= 0) {
      void read()
      return
    }
    const t = window.setTimeout(() => void read(), wait)
    return () => window.clearTimeout(t)
  }, [read, refreshKey])

  if (!view) return readError ? <EmptyState title="Merge request state unavailable" /> : null
  if (!view.supported) {
    return (
      <EmptyState title="Merge request not supported">
        {view.reason}
        {view.mr_url && (
          <>
            {' '}
            <a href={view.mr_url} target="_blank" rel="noreferrer">Open the merge request</a>
          </>
        )}
      </EmptyState>
    )
  }
  if (!view.mr) return <EmptyState title="No merge request yet" />

  const mr = view.mr
  const sha = view.sha ?? ''
  const open = view.mr_state === 'opened' || view.mr_state === 'open'
  const files = diff ? [...diff.committed, ...diff.uncommitted, ...diff.untracked] : []
  const outside = files.filter((f) => !f.in_scope).map((f) => f.path)
  const fileCount = files.length + (diff?.files_more ?? 0)

  const approve = async () => {
    setBusy(true)
    setFailure(null)
    const key = waitingKey({ kind: 'mr', action: 'approve', lane_id: lane.id, mr, title: '' })
    const items = snap.waiting?.items ?? []
    try {
      await laneApprove.approve(lane.id, sha)
      if (!mounted.current) return
      setConfirming(false)
      setApproved(true)
      void read()
      // The next merge request opens here (the shared step only knows decisions and questions).
      const next = neighbour(items, key, 'down', key)
      const to = next ? inboxOpen(next) : null
      if (to) go(withOpen(window.location.hash, to))
      else if (next) showWaiting(next)
      else go(withOpen(window.location.hash, undefined))
      void live.readWaiting()
    } catch (e) {
      if (!mounted.current) return
      setFailure(e instanceof ApiError || e instanceof Error ? e.message : String(e))
      setConfirming(false)
      await read()
    } finally {
      if (mounted.current) setBusy(false)
    }
  }

  // Nothing reviews a merge request on its own yet: the link asks the front desk, in the person's words.
  const requestReview = () => {
    prefillDraft(window.localStorage, draftKey(deskWorkspace(snap.status?.workspaces ?? [])), `Review merge request !${mr} on ${lane.branch} and tell me what needs a person.`)
    go(href.chat())
  }

  return (
    <section className="mr-approval" aria-label="Merge request approval" data-component="MrApproval">
      <h2 className="mr-approval-h">{view.approved || approved ? `Merge request !${mr} is approved` : `Approve merge request !${mr}?`}</h2>
      <p>{approvalSentence(view, lane, now)}</p>
      {diff && (
        <>
          <h4 className="mr-approval-sub">
            {diff.commits_total} {diff.commits_total === 1 ? 'commit' : 'commits'}, {fileCount} {fileCount === 1 ? 'file' : 'files'}
            {outside.length > 0 && `, ${outside.length} outside scope`}
          </h4>
          <ul className="mr-commits">
            {diff.commits.map((c) => <li key={c.sha}>{c.subject}</li>)}
          </ul>
        </>
      )}
      <div className="mr-review" data-component="AiReviewRow">
        <span className="mr-review-k">AI review</span>
        <b>unreviewed</b>
        <span className="faint">needs a person</span>
        <button type="button" className="sh-link mr-review-ask" onClick={requestReview}>Request AI review</button>
      </div>
      {outside.length > 0 && (
        <div className="sh-aside">
          <b>Outside scope:</b> {outside.join(', ')}. Review them in Changes before you approve.
        </div>
      )}
      {failure && <p className="tone-bad" role="alert">Not approved: {failure}</p>}
      {approved && <p role="status">Approved.</p>}
      {open && confirming && (
        <p className="mr-confirm" role="group" aria-label="Confirm approval">
          Approve !{mr} at <code>{shortSha(sha)}</code>? This is your sign-off on the forge for exactly this head. If the require-approval job has failed, it is retried so the pipeline can pass.
          <Button variant="attention" disabled={busy} onClick={() => void approve()}>Approve at {shortSha(sha)}</Button>
          <button type="button" className="sh-link mr-review-ask" disabled={busy} onClick={() => setConfirming(false)}>Not yet</button>
        </p>
      )}
      <div className="mr-actions">
        {open && !view.approved && !approved && !confirming && (
          <Button variant="attention" disabled={busy || !sha} onClick={() => setConfirming(true)}>Approve !{mr}</Button>
        )}
        <button type="button" className="sh-link" onClick={onReview}>Review changes</button>
        <span className="faint mr-next"><Kbd keys="]" />skip to next</span>
      </div>
    </section>
  )
}

/**
 * A lane's merge request in the inspector: `open=mr:N` is the merge request of lane N, as the
 * forge has it. The Approval tab holds the person's own Approve (the same action the lane's
 * Changes tab has); the other tabs are the lane's, so the merge request can be judged here.
 */
export function MrInspector({ open, chrome }: { open: Open; chrome?: PageChrome }) {
  const snap = useSnapshot()
  const now = useNow()
  const tab = mrTab(open.tab)
  const lane = snap.lanes.find((l) => l.id === open.id)
  const source = useMemo(() => laneSource(open.id), [open.id])
  const feedLast = snap.feed.at(-1)?.id ?? 0
  const counts = useLaneCounts(open.id, feedLast)
  const runCount = snap.runs.filter((r) => r.lane_id === open.id).length
  const tabBase = usePageTabBase()

  const select = (t: string) =>
    go(withOpen(window.location.hash, { kind: 'mr', id: open.id, ...(t === 'approval' ? {} : { tab: t }), ...(open.full ? { full: true } : {}) }))

  if (!lane) {
    return (
      <div className="inspector-body">
        {chrome && <PageHeader {...chrome} title={`MR of lane ${open.id}`} />}
        <p className="faint">{snap.ready ? `No open lane ${open.id}, so no merge request.` : 'Loading…'}</p>
        <p><a href={href.lane(open.id)}>Open the lane</a></p>
      </div>
    )
  }

  const items = [
    { id: 'approval', label: 'Approval', content: <MrApproval key={lane.id} lane={lane} refreshKey={feedLast} now={now} onReview={() => select('changes')} /> },
    {
      id: 'changes',
      label: tabLabel('Changes', counts.changes),
      content: <Changes laneId={lane.id} worktree={lane.worktree} editorUrl={null} refreshKey={feedLast} now={now} />,
    },
    { id: 'artifacts', label: tabLabel('Artifacts', counts.artifacts), content: <LaneArtifactsPanel laneId={lane.id} refreshKey={feedLast} /> },
    { id: 'conversation', label: 'Conversation', content: <Conversation source={source} refreshKey={feedLast} now={now} /> },
    { id: 'runs', label: tabLabel('Runs', runCount), content: <LaneRuns laneId={lane.id} /> },
  ]

  if (chrome) {
    const current = items.find((t) => t.id === tab) ?? items[0]
    const waiting = !!lane.mr && lane.mr_state !== 'merged' && lane.mr_state !== 'closed'
    return (
      <div className="inspector-body mr-inspector">
        <PageHeader
          {...chrome}
          glyph={waiting ? <StatusGlyph glyph="◆" tone="warn" label="waiting for your approval" /> : undefined}
          title={lane.mr ? `Merge request !${lane.mr}` : `Merge request of lane ${lane.id}`}
          meta={
            <>
              <span>{lane.repo}</span>
              <span>
                <code>{lane.branch}</code>
                {lane.base && <> onto <code>{lane.base}</code></>}
              </span>
              {lane.mr_state && <span>{lane.mr_state}</span>}
              {lane.pipeline ? <span>pipeline {lane.pipeline} {lane.pipeline_status}</span> : null}
            </>
          }
          actions={
            <Button variant="ghost" onClick={() => go(href.lane(lane.id))}>
              Open the lane page
            </Button>
          }
          tabs={<PageTabs tabs={items.map(({ id, label }) => ({ id, label }))} value={current?.id ?? tab} onChange={select} label="Merge request views" base={tabBase} />}
        />
        {current && (
          <div role="tabpanel" id={pageTabIds(tabBase, current.id).panel} aria-labelledby={pageTabIds(tabBase, current.id).tab} className="sh-tabpanel">
            {current.content}
          </div>
        )}
      </div>
    )
  }

  return (
    <div className="inspector-body mr-inspector">
      <Tabs items={items} value={tab} onChange={select} label="Merge request views" />
    </div>
  )
}
