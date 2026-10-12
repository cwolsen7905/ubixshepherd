import { useEffect, useMemo, useRef, useState } from 'react'
import { ApiError, laneClose } from '../api/client'
import type { LaneReview, LaneView } from '../api/types'
import { useLive } from '../state/context'
import '../styles/lane-close.css'

/** The least time between two reviews of the same repos: each one refreshes the remote. */
export const REVIEW_INTERVAL = 60_000

/**
 * What the daemon's review says of each open lane, by lane id: only lanes it judged
 * finished are returned. `refreshKey` changing asks for a fresh read, at most once an
 * interval. A failed read leaves what was known.
 */
export function useFinishedLanes(lanes: LaneView[], refreshKey: number | string): Map<number, LaneReview> {
  const open = useMemo(() => lanes.filter((l) => l.state === 'open'), [lanes])
  // The repos and lanes, as a string: the effect runs when they change, not on every render.
  const shape = open.map((l) => `${l.repo_id}:${l.id}:${l.name}`).sort().join(',')
  const [found, setFound] = useState<Map<number, LaneReview>>(new Map())
  const last = useRef(0)
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  useEffect(() => {
    if (shape === '') return
    let timer: number | undefined
    const read = () => {
      last.current = Date.now()
      const repos = [...new Set(open.map((l) => l.repo_id))]
      // A read in flight when the lanes or the feed move on is still the freshest answer.
      void Promise.all(repos.map((id) => laneClose.review(id).catch(() => null))).then((all) => {
        if (!mounted.current) return
        const next = new Map<number, LaneReview>()
        all.forEach((reviews, i) => {
          for (const r of Array.isArray(reviews) ? reviews : []) {
            const l = open.find((x) => x.repo_id === repos[i] && x.name === r.lane)
            if (l && r.verdict === 'finished') next.set(l.id, r)
          }
        })
        setFound(next)
      })
    }
    const wait = REVIEW_INTERVAL - (Date.now() - last.current)
    if (wait <= 0) read()
    else timer = window.setTimeout(read, wait)
    return () => window.clearTimeout(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- `shape` stands for `open`
  }, [shape, refreshKey])
  return found
}

/** "finished: ready to close", with the daemon's evidence on hover and below it in `detail`. */
export function FinishedMark({ review, lastRun, detail = false }: { review: LaneReview; lastRun?: string; detail?: boolean }) {
  const evidence = finishedEvidence(review, lastRun)
  return (
    <span className="finished-mark" title={evidence.join('\n')}>
      finished: ready to close
      {detail && <span className="finished-why faint"> ({evidence.join('; ')})</span>}
    </span>
  )
}

/** Why a lane counts as finished, in short phrases. */
export function finishedEvidence(review: LaneReview, lastRun?: string): string[] {
  const out = [review.why]
  if (lastRun) out.push(`last run ${lastRun}`)
  return out
}

/** The reason a refused close gives, or what went wrong. */
function reasonOf(e: unknown): { text: string; refused: boolean } {
  if (e instanceof ApiError) return { text: e.message, refused: e.status === 409 }
  return { text: e instanceof Error ? e.message : String(e), refused: false }
}

/**
 * Closes a lane by the daemon's own close, so its checks apply. A refusal is shown with
 * its reason; "Close anyway" is offered only behind a confirm that says what it does.
 * Disabled while a run is going in the lane.
 */
export function LaneCloseButton({ lane, running, onClosed }: { lane: LaneView; running: boolean; onClosed?: () => void }) {
  const live = useLive()
  const [busy, setBusy] = useState(false)
  const [refusal, setRefusal] = useState<string | null>(null)
  const [failure, setFailure] = useState<string | null>(null)
  const [confirming, setConfirming] = useState(false)
  const [done, setDone] = useState(false)

  const close = async (force: boolean) => {
    setBusy(true)
    setFailure(null)
    try {
      await laneClose.close(lane.id, force)
      setDone(true)
      setRefusal(null)
      setConfirming(false)
      await live.refresh()
      onClosed?.()
    } catch (e) {
      const r = reasonOf(e)
      if (r.refused) {
        setRefusal(r.text)
        setConfirming(false)
      } else setFailure(r.text)
    } finally {
      setBusy(false)
    }
  }

  if (done) return <span className="lane-close-done faint">Lane closed.</span>
  return (
    <span className="lane-close">
      <button
        type="button"
        className="lane-close-button"
        disabled={busy || running}
        title={running ? 'A run is going in this lane: stop it first.' : 'Close the lane: remove its worktree if it is clean and its work is in the base.'}
        onClick={() => void close(false)}
      >
        Close lane
      </button>
      {failure && <span className="lane-close-note tone-bad" role="alert">Could not close the lane: {failure}</span>}
      {refusal && (
        <span className="lane-close-note" role="alert">
          <span className="tone-bad">Not closed: </span>
          <span className="lane-close-reason">{refusal}</span>
          {!confirming && (
            <button type="button" className="link-button" disabled={busy || running} onClick={() => setConfirming(true)}>
              Close anyway…
            </button>
          )}
        </span>
      )}
      {refusal && confirming && (
        <span className="lane-close-confirm" role="group" aria-label="Confirm closing anyway">
          Closing anyway removes the worktree <code>{lane.worktree}</code> and discards any uncommitted changes in it. The
          branch <code>{lane.branch}</code> is kept unless git can see it merged, so commits on it are not lost.
          <button type="button" className="lane-close-button danger" disabled={busy || running} onClick={() => void close(true)}>
            Discard changes and close
          </button>
          <button type="button" className="link-button" onClick={() => setConfirming(false)}>
            Keep the lane
          </button>
        </span>
      )}
    </span>
  )
}
