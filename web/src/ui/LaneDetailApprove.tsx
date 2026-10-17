import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, laneApprove } from '../api/client'
import type { LaneApproval, LaneApproved } from '../api/types'
import { shortSha } from '../format'
import { Button } from './Button'
import { Dialog } from './Dialog'

/** The least time between two reads the feed triggers: a read asks the forge several things. */
const MIN_REREAD = 10_000

export interface LaneApprovalState {
  /** The forge's view of the merge request; null until read, or when it cannot be read. */
  view: LaneApproval | null
  /** An approval is on offer: a supported, open, unapproved merge request with a head to approve. */
  canApprove: boolean
  confirming: boolean
  setConfirming: (on: boolean) => void
  busy: boolean
  failure: string | null
  result: LaneApproved | null
  /** The person's own click, for exactly the head that was read. */
  approve: () => Promise<void>
}

/**
 * Reads a lane's merge request approval as the forge has it, and holds the approval itself, so
 * the header's Approve and the facts rail's state speak of one read. `refreshKey` moving reads
 * again, no sooner than MIN_REREAD. Off (`enabled` false) it reads nothing.
 */
export function useLaneApproval(laneId: number, refreshKey: number, enabled: boolean): LaneApprovalState {
  const [view, setView] = useState<LaneApproval | null>(null)
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState<string | null>(null)
  const [result, setResult] = useState<LaneApproved | null>(null)
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
      if (mounted.current) setView(v)
    } catch {
      if (mounted.current) setView(null)
    }
  }, [laneId])
  useEffect(() => {
    if (!enabled) return
    const wait = last.current === 0 ? 0 : MIN_REREAD - (Date.now() - last.current)
    if (wait <= 0) {
      void read()
      return
    }
    const t = window.setTimeout(() => void read(), wait)
    return () => window.clearTimeout(t)
  }, [enabled, read, refreshKey])

  const sha = view?.sha ?? ''
  const open = view?.mr_state === 'opened' || view?.mr_state === 'open'
  const canApprove = !!view && view.supported && !!view.mr && open && !view.approved && !result && sha !== ''
  const approve = async () => {
    setBusy(true)
    setFailure(null)
    try {
      setResult(await laneApprove.approve(laneId, sha))
      if (!mounted.current) return
      setConfirming(false)
      await read()
    } catch (e) {
      if (!mounted.current) return
      setFailure(e instanceof ApiError || e instanceof Error ? e.message : String(e))
      await read()
    } finally {
      if (mounted.current) setBusy(false)
    }
  }
  return { view, canApprove, confirming, setConfirming, busy, failure, result, approve }
}

/**
 * The header's Approve, the state's primary action on a lane to review: "Approve !41…" opens
 * the confirmation, which names the exact head the person signs off.
 */
export function ApproveAction({ approval }: { approval: LaneApprovalState }) {
  const { view, confirming, setConfirming, busy, failure } = approval
  if (!view?.mr) return null
  const sha = view.sha ?? ''
  return (
    <>
      <Button variant="attention" disabled={busy || !sha} onClick={() => setConfirming(true)}>
        Approve !{view.mr}…
      </Button>
      <Dialog
        open={confirming}
        onClose={() => setConfirming(false)}
        title={`Approve !${view.mr}?`}
        alert
        actions={
          <>
            <Button variant="ghost" disabled={busy} onClick={() => setConfirming(false)}>
              Not yet
            </Button>
            <Button variant="attention" disabled={busy} onClick={() => void approval.approve()}>
              Approve at {shortSha(sha)}
            </Button>
          </>
        }
      >
        <p>
          This is your sign-off on the forge, for exactly this head, <code>{shortSha(sha)}</code>. If the require-approval job has failed, it is retried so the pipeline can pass.
        </p>
        {failure && <p className="tone-bad" role="alert">Not approved: {failure}</p>}
      </Dialog>
    </>
  )
}
