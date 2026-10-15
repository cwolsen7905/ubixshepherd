import { useEffect, useRef, useState } from 'react'
import { ApiError, waitingOnYou } from '../api/client'
import type { WaitingItem } from '../api/types'
import { useLive, useSnapshot } from '../state/context'
import { href, refocus } from '../router'
import { showInThread, stepIndex, waitingKey } from '../model/waitingStep'
import { LaneApprove } from './LaneApprove'
import './WaitingOnYou.css'

const KIND_LABEL: Record<WaitingItem['kind'], string> = {
  decision: 'decision',
  mr: 'merge request',
  ship: 'to ship',
  next: 'next up',
  question: 'question',
  blocked: 'blocked',
  scope: 'scope',
  request: 'request',
}

const keyOf = waitingKey

/** A click on the link to the hash already shown fires no hashchange: ask the page to focus again. */
function again(to: string) {
  if (window.location.hash === to) refocus()
}

/**
 * One list of what needs the person's action: open decisions, merge requests held on their
 * approval, lanes with work to ship, and Next-up items proposed to them. It is a badge with
 * a count in the header and a dropdown with each item's action. Approving reuses the Lane
 * page's LaneApprove control (the person's own click, for the head they looked at);
 * shipping is the person's click on the lane_ship path.
 */
export function WaitingOnYou() {
  const live = useLive()
  // The list is the attention store's: this component does not poll.
  const list = useSnapshot().waiting
  const [open, setOpen] = useState(false)
  const [approving, setApproving] = useState<string | null>(null)
  const [shipping, setShipping] = useState<string | null>(null)
  const [notes, setNotes] = useState<Record<string, { text: string; bad: boolean }>>({})
  const [at, setAt] = useState<string | null>(null)
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  const read = () => live.readWaiting()

  const ship = async (it: WaitingItem) => {
    const key = keyOf(it)
    if (!it.lane_id) return
    setShipping(key)
    try {
      const r = await waitingOnYou.ship(it.lane_id)
      if (mounted.current) setNotes((n) => ({ ...n, [key]: { text: r.message || 'Shipped.', bad: false } }))
    } catch (e) {
      const text = e instanceof ApiError || e instanceof Error ? e.message : String(e)
      if (mounted.current) setNotes((n) => ({ ...n, [key]: { text: `Not shipped: ${text}`, bad: true } }))
    } finally {
      if (mounted.current) setShipping(null)
      void read()
    }
  }

  const count = list?.count ?? 0
  // Up and down step through every waiting item in the thread, read or not.
  const step = (dir: 'up' | 'down') => {
    const items = list?.items ?? []
    const i = stepIndex(items.map(keyOf), at, dir)
    const it = items[i]
    if (!it) return
    setAt(keyOf(it))
    showInThread(it)
  }
  if (!list) return null
  if (count === 0 && !open) return null

  return (
    <div className="waiting">
      <button
        type="button"
        className="waiting-toggle tone-warn"
        aria-expanded={open}
        aria-controls="waiting-panel"
        title="Everything that needs your action"
        onClick={() => setOpen((o) => !o)}
      >
        {count} waiting on you
      </button>
      {count > 0 && (
        <span className="waiting-step">
          <button type="button" aria-label="Previous item waiting on you" title="Previous waiting item" onClick={() => step('up')}>
            ▲
          </button>
          <button type="button" aria-label="Next item waiting on you" title="Next waiting item" onClick={() => step('down')}>
            ▼
          </button>
        </span>
      )}
      {open && (
        <ul id="waiting-panel" className="waiting-panel" aria-label="Waiting on you">
          {count === 0 && <li className="waiting-empty faint">Nothing waits on you.</li>}
          {list.items.map((it) => {
            const key = keyOf(it)
            const note = notes[key]
            const where = it.lane ? (it.repo ? `${it.repo}:${it.lane}` : it.lane) : it.repo
            return (
              <li key={key} className="waiting-item">
                <div className="waiting-head">
                  <span className="waiting-kind faint">{KIND_LABEL[it.kind]}</span>
                  <span className="waiting-title">
                    {it.title}
                    {where && <span className="faint"> · {where}</span>}
                  </span>
                  {it.action === 'answer' && (
                    <a href={href.decisions(it.decision_id)} onClick={() => again(href.decisions(it.decision_id))}>Answer</a>
                  )}
                  {it.action === 'route' && <a href={href.decisions()}>Route</a>}
                  {it.action === 'reply' && (
                    <button type="button" onClick={() => showInThread(it)}>Reply</button>
                  )}
                  {it.action === 'review' && (
                    <a href={href.next(it.next_id)} onClick={() => again(href.next(it.next_id))}>Review</a>
                  )}
                  {it.action === 'open' && it.lane_id ? <a href={href.lane(it.lane_id)}>Open lane</a> : null}
                  {it.action === 'approve' && it.lane_id ? (
                    <button type="button" aria-expanded={approving === key} onClick={() => setApproving(approving === key ? null : key)}>
                      Approve…
                    </button>
                  ) : null}
                  {it.action === 'ship' && it.lane_id ? (
                    <button type="button" disabled={shipping === key} onClick={() => void ship(it)}>
                      {shipping === key ? 'Shipping…' : 'Ship'}
                    </button>
                  ) : null}
                </div>
                {it.action === 'approve' && it.lane_id && approving === key && <LaneApprove laneId={it.lane_id} refreshKey={list.version ?? list.count} />}
                {note && (
                  <p className={`waiting-note${note.bad ? ' tone-bad' : ''}`} role={note.bad ? 'alert' : 'status'}>
                    {note.text}
                  </p>
                )}
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}
