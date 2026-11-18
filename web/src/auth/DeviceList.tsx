import { useCallback, useEffect, useState } from 'react'
import { since } from '../format'
import { Button, Confirm } from '../ui'
import { devices as api, signOutEverywhere as endAll, type Device } from './api'

type Ops = {
  list: () => Promise<Device[]>
  revoke: (id: string) => Promise<unknown>
  signOutEverywhere: () => Promise<unknown>
}

const live: Ops = { list: api.list, revoke: api.revoke, signOutEverywhere: endAll }

/**
 * The device tokens `shepherd login` made, each revocable, and "sign out everywhere". It
 * belongs in Settings of a hosted daemon. A device is listed by name and times only: the
 * daemon never sends a token or its hash.
 */
export function DeviceList({ ops = live, now = () => Date.now(), onSignedOut }: { ops?: Ops; now?: () => number; onSignedOut?: () => void }) {
  const [rows, setRows] = useState<Device[] | null>(null)
  const [failure, setFailure] = useState('')
  const [revoking, setRevoking] = useState<Device | null>(null)
  const [everywhere, setEverywhere] = useState(false)

  const load = useCallback(() => {
    ops.list().then(
      (d) => {
        setRows(d)
        setFailure('')
      },
      (e: unknown) => setFailure(e instanceof Error ? e.message : String(e)),
    )
  }, [ops])
  useEffect(load, [load])

  const fail = (e: unknown) => setFailure(e instanceof Error ? e.message : String(e))

  return (
    <section className="sh-card" data-component="DeviceList" aria-labelledby="device-list-h">
      <h2 id="device-list-h">Signed-in devices</h2>
      <p>Each command-line login has its own token. It stops after 30 days unused, and 90 days after you logged in however often it is used.</p>
      {failure && (
        <div className="sh-banner sh-banner--bad" role="alert">
          <span aria-hidden="true">✗ </span>
          {failure}
        </div>
      )}
      {rows === null && !failure && <p>Loading devices</p>}
      {rows?.length === 0 && <p>No device is signed in.</p>}
      {rows && rows.length > 0 && (
        <ul>
          {rows.map((d) => (
            <li key={d.id}>
              <strong>{d.name}</strong>
              <span> · signed in {since(d.created, now())} ago · last used {since(d.last_used, now())} ago</span>
              <span> · ends {new Date(d.expires).toISOString().slice(0, 10)} if unused, never after {new Date(d.max_expires).toISOString().slice(0, 10)}</span>{' '}
              <Button size="small" variant="danger" onClick={() => setRevoking(d)}>
                Revoke
              </Button>
            </li>
          ))}
        </ul>
      )}
      <Button variant="danger" onClick={() => setEverywhere(true)}>
        Sign out everywhere
      </Button>
      <Confirm
        open={revoking !== null}
        title={`Revoke ${revoking?.name ?? ''}?`}
        items={['That device must run shepherd login again.']}
        confirmLabel="Revoke"
        tone="danger"
        onCancel={() => setRevoking(null)}
        onConfirm={() => {
          const d = revoking
          setRevoking(null)
          if (d) ops.revoke(d.id).then(load, fail)
        }}
      />
      <Confirm
        open={everywhere}
        title="Sign out everywhere?"
        items={['Every browser session ends, this one included.', 'Every device token stops working.']}
        confirmLabel="Sign out everywhere"
        tone="danger"
        onCancel={() => setEverywhere(false)}
        onConfirm={() => {
          setEverywhere(false)
          ops.signOutEverywhere().then(() => onSignedOut?.(), fail)
        }}
      />
    </section>
  )
}
