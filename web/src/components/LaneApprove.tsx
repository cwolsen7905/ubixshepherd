import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, laneApprove } from '../api/client'
import type { LaneApproval, LaneApproved } from '../api/types'
import { shortSha } from '../format'
import { Button } from '../ui/Button'
import { OFFLINE_SEND } from '../ui/ContextComposer'
import { useOffline } from '../ui/offline'
import { EmptyState } from '../ui/EmptyState'
import '../styles/lane-approve.css'

/** The least time between two reads the feed triggers. */
const MIN_REREAD = 10_000

const PIPELINE_TONE: Record<string, string> = { success: 'tone-ok', failed: 'tone-bad', canceled: 'tone-bad' }

/**
 * The lane's merge request as the forge has it: who approved, the pipeline and its
 * require-approval job, and an Approve button that is the person's own click. The daemon
 * refuses it to every token but a browser session, and refuses a head that moved since
 * this was read. `refreshKey` changing reads the forge again.
 */
export function LaneApprove({ laneId, refreshKey }: { laneId: number; refreshKey?: number | string }) {
  const [view, setView] = useState<LaneApproval | null>(null)
  const [readError, setReadError] = useState<string | null>(null)
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState<string | null>(null)
  const [result, setResult] = useState<LaneApproved | null>(null)
  const { offline } = useOffline()
  const offlineTitle = offline ? OFFLINE_SEND.replace('Send', 'Approve') : undefined
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
    try {
      const v = await laneApprove.read(laneId)
      if (!mounted.current) return
      setView(v)
      setReadError(null)
    } catch (e) {
      if (mounted.current) setReadError(e instanceof Error ? e.message : String(e))
    }
  }, [laneId])
  useEffect(() => {
    // A read asks the forge several things: the feed moving does not read it again within a
    // moment of the last read.
    if (last.current === 0 || Date.now() - last.current >= MIN_REREAD) void read()
  }, [read, refreshKey])

  if (!view) {
    return readError ? <EmptyState title="Merge request state unavailable" /> : null
  }
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

  const open = view.mr_state === 'opened'
  const sha = view.sha ?? ''
  const approve = async () => {
    setBusy(true)
    setFailure(null)
    try {
      const r = await laneApprove.approve(laneId, sha)
      if (!mounted.current) return
      setResult(r)
      setConfirming(false)
      await read()
    } catch (e) {
      if (!mounted.current) return
      setFailure(e instanceof ApiError || e instanceof Error ? e.message : String(e))
      setConfirming(false)
      await read()
    } finally {
      if (mounted.current) setBusy(false)
    }
  }

  return (
    <section className="lane-approve" aria-label="Merge request approval">
      <p className="lane-approve-state">
        <a href={view.mr_url} target="_blank" rel="noreferrer">!{view.mr}</a>
        <span>{open ? 'open' : view.mr_state}</span>
        {view.approved ? (
          <span className="tone-ok">approved{view.approved_by?.length ? ` by ${view.approved_by.join(', ')}` : ''}</span>
        ) : (
          <span className="tone-warn">not approved</span>
        )}
        {view.pipeline_status && (
          <span className={PIPELINE_TONE[view.pipeline_status]}>
            pipeline {view.pipeline_url ? <a href={view.pipeline_url} target="_blank" rel="noreferrer">{view.pipeline_status}</a> : view.pipeline_status}
          </span>
        )}
        {view.require_approval && (
          <span className={PIPELINE_TONE[view.require_approval]}>require-approval {view.require_approval}</span>
        )}
        {sha && <span className="faint">head <code>{shortSha(sha)}</code></span>}
      </p>
      {open && !view.approved && !confirming && (
        <Button variant="primary" disabled={busy || !sha || offline} title={offlineTitle} onClick={() => setConfirming(true)}>
          Approve !{view.mr}…
        </Button>
      )}
      {open && confirming && (
        <p className="lane-approve-confirm" role="group" aria-label="Confirm approval">
          Approve !{view.mr} at <code>{shortSha(sha)}</code>? This is your sign-off on the forge, for exactly this head. If the
          require-approval job has failed, it is retried so the pipeline can pass.
          <Button variant="primary" disabled={busy || offline} title={offlineTitle} onClick={() => void approve()}>
            Approve at {shortSha(sha)}
          </Button>
          <button type="button" className="link-button" disabled={busy} onClick={() => setConfirming(false)}>
            Not yet
          </button>
        </p>
      )}
      {failure && <p className="lane-approve-note tone-bad" role="alert">Not approved: {failure}</p>}
      {result && !failure && (
        <p className="lane-approve-note" role="status">
          Approved.{' '}
          {result.retried
            ? 'The require-approval job was retried.'
            : result.retry_note
              ? `The pipeline was left as it was: ${result.retry_note}.`
              : ''}
        </p>
      )}
    </section>
  )
}
