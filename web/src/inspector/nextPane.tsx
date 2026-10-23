import { useState } from 'react'
import { ApiError, nextUp } from '../api/client'
import { advanceFrom } from '../chat/strip'
import { openHref } from '../components/BacklogOpen'
import { since } from '../format'
import { useNow } from '../hooks'
import { draftKey, prefillDraft } from '../model/chatDraft'
import { NEXT_MARKS, canMove, canStart, deskWorkspace, planLabel, stateActions, startText } from '../model/next'
import { waitingKey } from '../model/waitingStep'
import { go, href } from '../router'
import { useLive, useSnapshot } from '../state/context'
import { Button, Chip, OFFLINE_SEND, StatusGlyph, useOffline } from '../ui'
import type { NextUpdateBody } from '../api/types'

const reason = (e: unknown) => (e instanceof ApiError || e instanceof Error ? e.message : String(e))

/**
 * A next-up item in the side panel: title, repo, state, note, plan doc, and its lane and merge
 * request if it has them, with the moves the Next up page offers (start, approve, defer, drop,
 * move, link to a lane). The page is its "Open as page".
 */
export function NextPane({ id }: { id: number }) {
  const snap = useSnapshot()
  const live = useLive()
  const now = useNow()
  const { offline } = useOffline()
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState('')
  const it = snap.next.find((x) => x.id === id)
  if (!it) {
    return (
      <p className="inspector-body" role="status">
        {snap.ready ? <>Next-up item {id} is not on the open list any more: it is done or dropped. <a href={href.next(id)}>Find it on the Next up page</a>.</> : 'Reading the item…'}
      </p>
    )
  }
  const mark = it.proposed ? { glyph: '≡', tone: 'warn' as const, label: 'proposed, waiting on you' } : NEXT_MARKS[it.state]
  const movable = it.stored_state === 'approved' || it.stored_state === 'planned' || it.stored_state === 'deferred'
  const peers = snap.next.filter((x) => x.stored_state === it.stored_state)
  const lanes = snap.lanes.filter((l) => l.state === 'open' && (!it.repo || l.repo === it.repo))
  const repoId = snap.lanes.find((l) => l.repo === it.repo)?.repo_id
  const off = offline ? OFFLINE_SEND : undefined

  const update = async (body: NextUpdateBody) => {
    setBusy(true)
    setProblem('')
    try {
      await nextUp.update(id, body)
      await live.settle()
      // Approving or dropping a proposal clears it from the waiting list: open the next one.
      const items = snap.waiting?.items ?? []
      const mine = items.find((w) => w.next_id === id)
      if (mine && body.state && body.state !== 'planned') advanceFrom(items, waitingKey(mine))
    } catch (e) {
      setProblem(reason(e))
    } finally {
      setBusy(false)
    }
  }
  const start = () => {
    prefillDraft(window.localStorage, draftKey(deskWorkspace(snap.status?.workspaces ?? [])), startText(it))
    go(href.chat())
  }

  return (
    <div className="inspector-body sh-next-pane" data-component="NextPane">
      <h3 className="sh-request-h">#{it.id} {it.title}</h3>
      <p className="sh-request-state">
        <StatusGlyph glyph={mark.glyph} tone={mark.tone === 'warn' ? 'warn' : 'idle'} label={mark.label} /> <b>{mark.label}</b>
        {it.proposed && <Chip>proposed</Chip>}
        <span className="sh-faint"> · {it.proposed ? 'proposed' : 'added'} by {it.source} · {since(it.created, now) || 'now'}</span>
      </p>
      <dl className="sh-request-facts">
        <dt>Repo</dt>
        <dd>{it.repo || '—'}</dd>
        <dt>Plan doc</dt>
        <dd>
          {it.plan ? (repoId ? <a href={href.files({ repo: repoId })} title={it.plan}><code>{planLabel(it.plan)}</code></a> : <code title={it.plan}>{planLabel(it.plan)}</code>) : '—'}
        </dd>
        <dt>Lane</dt>
        <dd>{it.lane_id ? <a href={openHref({ kind: 'lane', id: it.lane_id })}>{it.lane_name ?? it.lane}</a> : (it.lane ?? '—')}</dd>
        <dt>Merge request</dt>
        <dd>
          {it.mr ? (it.mr_url ? <a href={it.mr_url} target="_blank" rel="noreferrer">MR {it.mr}</a> : `MR ${it.mr}`) : '—'}
          {it.mr_state && <span className="sh-faint"> ({it.mr_state})</span>}
        </dd>
      </dl>
      {it.note && (
        <>
          <h3 className="sh-request-h">Note</h3>
          <p>{it.note}</p>
        </>
      )}
      {it.why && <p><span className="sh-tag">{it.why}</span></p>}

      <div className="sh-next-actions">
        {canStart(it) && <Button variant="primary" disabled={busy} onClick={start}>Start</Button>}
        {it.proposed ? (
          <>
            <Button variant="attention" disabled={busy || !!off} title={off} onClick={() => void update({ state: 'approved' })}>Approve and queue</Button>
            <Button disabled={busy || !!off} title={off} onClick={() => void update({ state: 'deferred' })}>Defer</Button>
            <Button disabled={busy || !!off} title={off} onClick={() => void update({ state: 'dropped' })}>Drop</Button>
          </>
        ) : (
          stateActions(it).map((a) => (
            <Button key={a.state} variant={a.state === 'approved' ? 'attention' : 'secondary'} disabled={busy || !!off} title={off} onClick={() => void update({ state: a.state })}>
              {a.label}
            </Button>
          ))
        )}
        {movable && (
          <>
            <Button variant="ghost" disabled={busy || !!off || !canMove(peers, it, 'up')} aria-label={`Move ${it.title} up`} onClick={() => void update({ move: 'up' })}>↑</Button>
            <Button variant="ghost" disabled={busy || !!off || !canMove(peers, it, 'down')} aria-label={`Move ${it.title} down`} onClick={() => void update({ move: 'down' })}>↓</Button>
          </>
        )}
        {!it.lane_id && movable && lanes.length > 0 && (
          <select
            className="sh-input"
            disabled={busy || !!off}
            value=""
            aria-label="Link to lane"
            onChange={(e) => {
              const lane = Number(e.target.value)
              if (lane > 0) void update({ lane_id: lane })
            }}
          >
            <option value="">Link to lane…</option>
            {lanes.map((l) => <option key={l.id} value={l.id}>{l.repo}/{l.name}</option>)}
          </select>
        )}
      </div>
      {problem && <p className="tone-bad" role="alert">{problem}</p>}
    </div>
  )
}
