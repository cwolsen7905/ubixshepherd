import { BackLink } from '../components/BackLink'
import { DaemonDetail, loadDaemon, useDaemonDetail, type Loaded } from '../components/DaemonStatus'
import { useNow } from '../hooks'
import { href } from '../router'
import { useChatLink } from '../shell/chatLink'
import { StatusGlyph } from '../ui/StatusGlyph'
import { useSnapshot } from '../state/context'
import './Status.css'

/** The full status of the daemon, this browser and the chat stream: what the rail's popover only summarises. */
export function StatusPage({ load = loadDaemon, now: nowProp }: { load?: () => Promise<Loaded>; now?: number } = {}) {
  const snap = useSnapshot()
  const chat = useChatLink()
  const ticking = useNow(10_000)
  const { data, failed } = useDaemonDetail(true, load)
  return (
    <div className="page sh-status-page">
      <header className="page-head sh-status-head">
        <BackLink fallback={href.settings()} />
        <h1>Status</h1>
        <span className="sh-status-live" role="status">
          {snap.error !== null ? (
            <><StatusGlyph glyph="✗" tone="bad" /> Offline · retrying</>
          ) : (
            <><StatusGlyph glyph="●" tone="ok" /> Online · updates live</>
          )}
        </span>
      </header>
      <div className="sh-status-grid">
        <DaemonDetail
          down={snap.error !== null}
          error={snap.error}
          downSince={snap.downSince}
          lastEvent={snap.feed.at(-1)?.created}
          chat={chat}
          data={data}
          failed={failed}
          now={nowProp ?? ticking}
        />
      </div>
    </div>
  )
}
