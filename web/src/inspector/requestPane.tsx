import { useMemo, useState } from 'react'
import { ApiError, requestActions } from '../api/client'
import type { RequestView } from '../api/types'
import { Markdown } from '../chat/md'
import { since } from '../format'
import { useNow } from '../hooks'
import { advanceFrom } from '../chat/strip'
import { waitingKey } from '../model/waitingStep'
import { go, withOpen } from '../router'
import { openHref } from '../components/BacklogOpen'
import { useLive, useSnapshot } from '../state/context'
import { Button, Confirm, OFFLINE_SEND, StatusGlyph, useOffline } from '../ui'

const reason = (e: unknown) => (e instanceof ApiError || e instanceof Error ? e.message : String(e))

/** The words for a request's state, for the one line under its text. */
const STATE_WORDS: Record<string, string> = {
  needs_routing: 'needs routing',
  pending: 'queued',
  routed: 'routed, waiting for a reply',
  reply_ready: 'reply ready',
  replied: 'replied',
  failed: 'failed',
  closed: 'closed',
}

type Target = { id: number; name: string; repo: string; state: string }

/**
 * The lanes a request can go to, split by whether the daemon can find them by name alone: it
 * takes the asker's repo first, else the one open lane of that name elsewhere. A name held by
 * several other repos, or shadowed by the asker's repo, cannot be chosen from here.
 */
export function routeTargets(q: RequestView, lanes: Target[]): { ok: Target[]; ambiguous: Target[] } {
  const open = lanes.filter((l) => l.state === 'open' && !(l.repo === q.repo && l.name === q.from_lane))
  const ok: Target[] = []
  const ambiguous: Target[] = []
  for (const l of open) {
    const same = open.filter((x) => x.name === l.name)
    const reachable = l.repo === q.repo || (same.length === 1 && !lanes.some((x) => x.state === 'open' && x.repo === q.repo && x.name === l.name))
    ;(reachable ? ok : ambiguous).push(l)
  }
  const order = (a: Target, b: Target) => Number(b.repo === q.repo) - Number(a.repo === q.repo) || a.repo.localeCompare(b.repo) || a.name.localeCompare(b.name)
  return { ok: ok.sort(order), ambiguous: ambiguous.sort(order) }
}

/**
 * A request between lanes that needs routing, in the side panel: what was asked, who asked,
 * its state, and the person's two moves: send it to a lane, or close it with a reason.
 */
export function RequestPane({ id }: { id: number }) {
  const snap = useSnapshot()
  const live = useLive()
  const now = useNow()
  const offline = useOffline()
  const q = snap.requests.find((x) => x.id === id)
  const { ok: targets, ambiguous } = useMemo(() => (q ? routeTargets(q, snap.lanes) : { ok: [], ambiguous: [] }), [q, snap.lanes])
  const [lane, setLane] = useState('')
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState('')
  const [closing, setClosing] = useState(false)
  const [why, setWhy] = useState('')

  if (!q) {
    return (
      <p className="inspector-body" role="status">
        {snap.ready ? `Request ${id} is not waiting on you any more. It was routed, answered or closed.` : 'Reading the request…'}
      </p>
    )
  }

  const act = async (run: () => Promise<unknown>) => {
    setBusy(true)
    setProblem('')
    try {
      await run()
      setClosing(false)
      await live.settle()
      // The request's own row in the waiting list (its key is the lane's, not the request's).
      const items = snap.waiting?.items ?? []
      const mine = items.find((it) => it.request_id === id)
      if (mine) advanceFrom(items, waitingKey(mine))
      else go(withOpen(window.location.hash, undefined))
    } catch (e) {
      setProblem(reason(e))
    } finally {
      setBusy(false)
    }
  }
  const target = targets.find((l) => String(l.id) === lane)
  const off = offline.offline ? OFFLINE_SEND : undefined
  const asker = snap.lanes.find((l) => l.repo === q.repo && l.name === q.from_lane)

  return (
    <div className="inspector-body sh-request-pane" data-component="RequestPane">
      <p className="sh-request-state">
        <StatusGlyph glyph="!" tone="warn" label="request" /> <b>{STATE_WORDS[q.state] ?? q.state}</b>
        <span className="sh-faint"> · {q.kind} · {since(q.created, now) || 'now'}</span>
      </p>
      <dl className="sh-request-facts">
        <dt>From</dt>
        <dd>
          {asker ? <a href={openHref({ kind: 'lane', id: asker.id })}>{q.repo}:{q.from_lane}</a> : `${q.repo}:${q.from_lane}`}
          {q.from_agent && <span className="sh-faint"> ({q.from_agent})</span>}
        </dd>
        <dt>Run</dt>
        <dd><a href={openHref({ kind: 'run', id: q.from_run })}>run {q.from_run}</a></dd>
        {q.lane && (
          <>
            <dt>Addressed to</dt>
            <dd>{q.lane}{q.agent && <span className="sh-faint"> ({q.agent})</span>}</dd>
          </>
        )}
      </dl>
      <h3 className="sh-request-h">Asked</h3>
      <Markdown text={q.message} />
      {q.note && <p className="sh-faint">{q.note}</p>}

      <h3 className="sh-request-h">Route it</h3>
      {targets.length === 0 ? (
        <p className="sh-faint">No other open lane to send it to. Open one first, or close the request.</p>
      ) : (
        <div className="sh-request-route">
          <label>
            <span className="visually-hidden">Lane to route to</span>
            <select className="sh-input" value={target ? lane : ''} disabled={busy} onChange={(e) => setLane(e.target.value)} aria-label="Lane to route to">
              <option value="">Choose a lane…</option>
              {targets.map((l) => (
                <option key={l.id} value={l.id}>{l.repo}:{l.name}</option>
              ))}
            </select>
          </label>
          <Button variant="attention" disabled={busy || !target || !!off} title={off} onClick={() => void act(() => requestActions.route(id, { lane: target?.name ?? '' }))}>
            {busy ? 'Routing…' : 'Route request'}
          </Button>
        </div>
      )}
      {ambiguous.length > 0 && (
        <p className="sh-faint">
          Not offered, because the daemon routes by lane name and the name is in more than one repo: {ambiguous.map((l) => `${l.repo}:${l.name}`).join(', ')}. Route it after one of them closes, or close this request.
        </p>
      )}
      {target && target.repo !== q.repo && <p className="sh-faint">That lane is in another repo: {target.repo}.</p>}
      {problem && <p className="tone-bad" role="alert">{problem}</p>}
      <p>
        <Button variant="ghost" disabled={busy} onClick={() => setClosing(true)}>Close without routing</Button>
      </p>
      <Confirm
        open={closing}
        title={`Close request ${id}`}
        items={['The asking agent is not answered.', 'Nothing is deleted; the request stays in the history.']}
        confirmLabel="Close request"
        tone="danger"
        onConfirm={() => void act(() => requestActions.close(id, why.trim()))}
        onCancel={() => setClosing(false)}
      >
        <label>
          Reason (optional)
          <textarea className="sh-input" rows={3} value={why} onChange={(e) => setWhy(e.target.value)} />
        </label>
      </Confirm>
    </div>
  )
}
