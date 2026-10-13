import { useCallback, useEffect, useState } from 'react'
import { updateCheck } from '../api/client'
import type { UpdateStatus } from '../api/types'
import './UpdateNotice.css'

/** How often the notice asks the daemon, which answers from its own daily check. */
export const REFRESH_MS = 10 * 60 * 1000
/** How often it asks while an upgrade is under way (or the daemon is restarting). */
export const BUSY_REFRESH_MS = 2000

interface Props {
  load?: () => Promise<UpdateStatus>
  dismiss?: (version: string) => Promise<UpdateStatus>
  upgrade?: (version: string, now: boolean) => Promise<UpdateStatus>
}

/** Only a web link is ever followed from the notice, whatever the release says. */
const safeLink = (url: string | undefined) => (url && /^https:\/\//i.test(url) ? url : null)

const reason = (e: unknown) => (e instanceof Error ? e.message : String(e))

const busy = (s: UpdateStatus | null) => {
  const state = s?.upgrade?.state
  return state === 'installing' || state === 'installed' || state === 'restarting'
}

/**
 * A slim, dismissable notice that a newer Shepherd release exists, with an Upgrade button
 * when the daemon can install it itself. The daemon decides whether to show it (`show`):
 * its check is on, there is a release the person has not dismissed, and it is newer than
 * this build, or this build's version is unknown. A failed read shows nothing: a notice is
 * never worth an error, and while the daemon restarts onto an upgrade it is expected.
 */
export function UpdateNotice({
  load = updateCheck.status,
  dismiss = updateCheck.dismiss,
  upgrade = updateCheck.upgrade,
}: Props) {
  const [st, setSt] = useState<UpdateStatus | null>(null)
  const [failed, setFailed] = useState<string | null>(null)
  const [working, setWorking] = useState(false)
  const fast = busy(st)

  useEffect(() => {
    let live = true
    const read = () =>
      load().then(
        (s) => live && setSt(s),
        () => undefined,
      )
    void read()
    const t = window.setInterval(() => void read(), fast ? BUSY_REFRESH_MS : REFRESH_MS)
    return () => {
      live = false
      window.clearInterval(t)
    }
  }, [load, fast])

  const hide = useCallback(() => {
    if (!st?.latest) return
    setFailed(null)
    dismiss(st.latest).then(setSt, (e: unknown) => setFailed(`Could not dismiss: ${reason(e)}`))
  }, [st, dismiss])

  const start = useCallback(
    (now: boolean) => {
      const version = st?.upgrade?.state === 'installed' ? st.upgrade.target : st?.latest
      if (!version) return
      setFailed(null)
      setWorking(true)
      upgrade(version, now).then(
        (s) => {
          setSt(s)
          setWorking(false)
        },
        (e: unknown) => {
          setFailed(`Could not upgrade: ${reason(e)}`)
          setWorking(false)
        },
      )
    },
    [st, upgrade],
  )

  if (!st?.show || !st.latest) return null
  const link = safeLink(st.url)
  const up = st.upgrade
  const state = up?.state ?? 'idle'
  const waiting = state === 'installing' || state === 'restarting'
  return (
    <div className="banner update-notice" role="status">
      <span className="update-notice-text">
        {st.current_known ? (
          <>
            Shepherd <b>{st.latest}</b> is available (this is {st.current}).
          </>
        ) : (
          <>
            The latest Shepherd release is <b>{st.latest}</b>. This build is {st.current}, so it is not compared.
          </>
        )}
        {link && (
          <>
            {' '}
            <a href={link} target="_blank" rel="noreferrer noopener">
              See the release
            </a>
          </>
        )}
        {state === 'installing' && <> Installing {up?.target}…</>}
        {state === 'restarting' && <> Restarting onto {up?.target}…</>}
        {state === 'installed' && (
          <>
            {' '}
            Installed: the daemon restarts when no agent is running
            {up && up.active_runs > 0 ? ` (${up.active_runs} running now)` : ''}.
          </>
        )}
        {(state === 'failed' || state === 'rolled_back') && up?.error && (
          <span className="update-notice-error"> The upgrade failed: {up.error}</span>
        )}
        {state !== 'installing' && state !== 'restarting' && state !== 'installed' && !up?.can_upgrade && up?.hint && (
          <> {up.hint}.</>
        )}
      </span>
      {failed && <span className="update-notice-error">{failed}</span>}
      {up?.can_upgrade && (state === 'idle' || state === 'failed' || state === 'rolled_back') && (
        <button type="button" className="link-button" disabled={working} onClick={() => start(false)}>
          Upgrade
        </button>
      )}
      {state === 'installed' && (
        <button type="button" className="link-button" disabled={working} onClick={() => start(true)}>
          {up && up.active_runs > 0
            ? `Restart now (interrupts ${up.active_runs} running ${up.active_runs === 1 ? 'agent' : 'agents'})`
            : 'Restart now'}
        </button>
      )}
      {!waiting && state !== 'installed' && (
        <button type="button" className="link-button" onClick={hide}>
          Dismiss
        </button>
      )}
    </div>
  )
}
