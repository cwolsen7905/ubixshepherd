import { useCallback, useEffect, useState } from 'react'
import { updateCheck } from '../api/client'
import type { UpdateStatus } from '../api/types'
import './UpdateNotice.css'

/** How often the notice asks the daemon, which answers from its own daily check. */
export const REFRESH_MS = 10 * 60 * 1000

interface Props {
  load?: () => Promise<UpdateStatus>
  dismiss?: (version: string) => Promise<UpdateStatus>
}

/** Only a web link is ever followed from the notice, whatever the release says. */
const safeLink = (url: string | undefined) => (url && /^https:\/\//i.test(url) ? url : null)

/**
 * A slim, dismissable notice that a newer Shepherd release exists. The daemon decides
 * whether to show it (`show`): its check is on, there is a release the person has not
 * dismissed, and it is newer than this build, or this build's version is unknown. A
 * failed read shows nothing: a notice is never worth an error.
 */
export function UpdateNotice({ load = updateCheck.status, dismiss = updateCheck.dismiss }: Props) {
  const [st, setSt] = useState<UpdateStatus | null>(null)
  const [failed, setFailed] = useState<string | null>(null)

  useEffect(() => {
    let live = true
    const read = () =>
      load().then(
        (s) => live && setSt(s),
        () => undefined,
      )
    void read()
    const t = window.setInterval(() => void read(), REFRESH_MS)
    return () => {
      live = false
      window.clearInterval(t)
    }
  }, [load])

  const hide = useCallback(() => {
    if (!st?.latest) return
    setFailed(null)
    dismiss(st.latest).then(setSt, (e: unknown) => setFailed(e instanceof Error ? e.message : String(e)))
  }, [st, dismiss])

  if (!st?.show || !st.latest) return null
  const link = safeLink(st.url)
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
      </span>
      {failed && <span className="update-notice-error">Could not dismiss: {failed}</span>}
      <button type="button" className="link-button" onClick={hide}>
        Dismiss
      </button>
    </div>
  )
}
