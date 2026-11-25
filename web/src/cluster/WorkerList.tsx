import { useCallback, useEffect, useState } from 'react'
import { since } from '../format'
import { Button, Confirm } from '../ui'
import { StatusGlyph } from '../ui/StatusGlyph'
import { cluster as api, type Worker, type WorkerCode } from './api'

type Ops = {
  list: () => Promise<Worker[]>
  add: (name: string) => Promise<WorkerCode>
  revoke: (name: string) => Promise<unknown>
  remove: (name: string) => Promise<unknown>
  confirm: (name: string) => Promise<unknown>
}

const live: Ops = {
  list: () => api.get().then((c) => c.workers ?? []),
  add: api.add,
  revoke: api.revoke,
  remove: api.remove,
  confirm: api.confirm,
}

/** How often the list reloads, so a laptop coming online shows without a refresh. */
const REFRESH_MS = 10_000

type Pending = { worker: Worker; act: 'revoke' | 'remove' }

/**
 * The machines linked to this controller: each with whether it is connected, since when,
 * its version and daemon id, and the runs going and items waiting there. Adding one shows
 * its one-time enrolment code, once. It belongs in Settings of a controller.
 */
export function WorkerList({ ops = live, now = () => Date.now() }: { ops?: Ops; now?: () => number }) {
  const [rows, setRows] = useState<Worker[] | null>(null)
  const [failure, setFailure] = useState('')
  const [name, setName] = useState('')
  const [code, setCode] = useState<WorkerCode | null>(null)
  const [pending, setPending] = useState<Pending | null>(null)

  const load = useCallback(() => {
    ops.list().then(
      (m) => {
        setRows(m)
        setFailure('')
      },
      (e: unknown) => setFailure(e instanceof Error ? e.message : String(e)),
    )
  }, [ops])
  useEffect(() => {
    load()
    const t = setInterval(load, REFRESH_MS)
    return () => clearInterval(t)
  }, [load])

  const fail = (e: unknown) => setFailure(e instanceof Error ? e.message : String(e))

  return (
    <section className="sh-card" data-component="WorkerList" aria-labelledby="worker-list-h">
      <h2 id="worker-list-h">Workers</h2>
      <p>Machines that link to this controller. Each dials out to it, so none needs to be reachable from here.</p>
      {failure && (
        <div className="sh-banner sh-banner--bad" role="alert">
          <span aria-hidden="true">✗ </span>
          {failure}
        </div>
      )}
      {rows === null && !failure && <p>Loading workers</p>}
      {rows?.length === 0 && <p>No worker yet. Add one, then run shepherd cluster join on that machine with the code.</p>}
      {rows && rows.length > 0 && (
        <ul>
          {rows.map((m) => (
            <li key={m.name}>
              <strong>{m.name}</strong> <WorkerState m={m} now={now()} />
              {m.online && (
                <span>
                  {' '}· {m.version ? `v${m.version.replace(/^v/, '')} · ` : ''}api {m.api_level} · {m.runs_going} running, {m.waiting} waiting
                </span>
              )}
              {m.daemon_id && <span title="The worker's daemon id"> · id <code>{m.daemon_id}</code></span>}{' '}
              {m.pending_id && (
                <Button size="small" onClick={() => ops.confirm(m.name).then(load, fail)}>
                  Confirm new id
                </Button>
              )}{' '}
              {m.enrolled && (
                <Button size="small" variant="danger" onClick={() => setPending({ worker: m, act: 'revoke' })}>
                  Revoke
                </Button>
              )}{' '}
              <Button size="small" variant="danger" onClick={() => setPending({ worker: m, act: 'remove' })}>
                Remove
              </Button>
            </li>
          ))}
        </ul>
      )}
      <form
        onSubmit={(e) => {
          e.preventDefault()
          const n = name.trim()
          if (!n) return
          ops.add(n).then(
            (c) => {
              setCode(c)
              setName('')
              load()
            },
            fail,
          )
        }}
      >
        <label htmlFor="worker-name">Worker name</label>{' '}
        <input id="worker-name" type="text" value={name} placeholder="work" onChange={(e) => setName(e.target.value)} />{' '}
        <Button type="submit" disabled={name.trim() === ''}>
          Add worker
        </Button>
      </form>
      {code && (
        <div className="sh-banner" role="status">
          <p>
            Enrolment code for {code.name}, shown once. It works once, until {new Date(code.expires).toLocaleTimeString([], { hour12: false })}:
          </p>
          <p>
            <code>{code.code}</code>
          </p>
          <p>On that machine: shepherd cluster join this controller&apos;s https address and the code.</p>
          <Button size="small" onClick={() => setCode(null)}>
            Hide the code
          </Button>
        </div>
      )}
      <Confirm
        open={pending !== null}
        title={`${pending?.act === 'remove' ? 'Remove' : 'Revoke'} ${pending?.worker.name ?? ''}?`}
        items={
          pending?.act === 'remove'
            ? ['Its link closes and the worker is forgotten.']
            : ['Its link closes and its credential stops working. Add it again for a new code.']
        }
        confirmLabel={pending?.act === 'remove' ? 'Remove' : 'Revoke'}
        tone="danger"
        onCancel={() => setPending(null)}
        onConfirm={() => {
          const p = pending
          setPending(null)
          if (p) (p.act === 'remove' ? ops.remove : ops.revoke)(p.worker.name).then(load, fail)
        }}
      />
    </section>
  )
}

function WorkerState({ m, now }: { m: Worker; now: number }) {
  if (m.pending_id) return <span><StatusGlyph glyph="✗" tone="bad" /> refused: its daemon id changed</span>
  if (m.online) return <span><StatusGlyph glyph="●" tone="live" /> online{m.since ? ` since ${since(m.since, now)} ago` : ''}</span>
  if (!m.enrolled) return <span><StatusGlyph glyph="○" /> {m.code_expires ? 'waiting to enrol' : 'not enrolled'}</span>
  return <span><StatusGlyph glyph="○" /> offline{m.since ? `, last seen ${since(m.since, now)} ago` : ', never linked'}</span>
}
