import { useCallback, useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { daemonInfo } from '../api/client'
import type { Status, StatusDetail, WebSession } from '../api/types'
import { age, clockTime, shortSha, since, usd } from '../format'
import { useNow } from '../hooks'

export interface Loaded {
  status: Status
  session: WebSession | null
}

/** Reads the daemon's detail and this browser's session; a session that cannot be read is null. */
export function loadDaemon(): Promise<Loaded> {
  return Promise.all([daemonInfo.status(), daemonInfo.session().catch(() => null)]).then(([status, session]) => ({ status, session }))
}

/** How often the open popover reads again. */
export const REFRESH_MS = 5000

interface Props {
  /** The live connection: why it is down (null when up), since when, and the newest feed item's time. */
  error: string | null
  downSince: number | null
  ready: boolean
  lastEvent?: string
  load?: () => Promise<Loaded>
  /** The time now in ms; the clock ticks by itself when omitted. */
  now?: number
}

/**
 * The bar's liveness dot. Hovering, focusing or tapping it opens a popover with what the
 * daemon is and is doing; Escape closes it. The detail is read only while it is open.
 */
export function DaemonStatus({ error, downSince, ready, lastEvent, load = loadDaemon, now: nowProp }: Props) {
  const ticking = useNow(10_000)
  const now = nowProp ?? ticking
  const id = useId()
  const [open, setOpen] = useState(false)
  const [pinned, setPinned] = useState(false)
  const [data, setData] = useState<Loaded | null>(null)
  const [failed, setFailed] = useState<string | null>(null)
  const box = useRef<HTMLSpanElement>(null)

  const close = useCallback(() => {
    setOpen(false)
    setPinned(false)
  }, [])

  useEffect(() => {
    if (!open) return
    let live = true
    const read = () =>
      load().then(
        (d) => live && (setData(d), setFailed(null)),
        (e: unknown) => live && setFailed(e instanceof Error ? e.message : String(e)),
      )
    void read()
    const t = window.setInterval(() => void read(), REFRESH_MS)
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && close()
    const onDown = (e: Event) => box.current && !box.current.contains(e.target as Node) && close()
    document.addEventListener('keydown', onKey)
    document.addEventListener('pointerdown', onDown)
    return () => {
      live = false
      window.clearInterval(t)
      document.removeEventListener('keydown', onKey)
      document.removeEventListener('pointerdown', onDown)
    }
  }, [open, load, close])

  const down = error !== null
  
  // Get the forge poll time if available
  const forgePollTime = data?.status.detail?.forge?.last_poll
  const forgePollAge = forgePollTime ? since(forgePollTime, now) : null
  
  // Text for the status indicator: offline (error), connecting (not ready), or live
  const statusText = down ? 'Daemon unreachable' : ready ? 'Daemon live' : 'Daemon connecting'
  // "forge polled Xs ago" — avoid "polled just now ago"
  const forgeText = forgePollAge
    ? forgePollAge === 'just now'
      ? 'forge polled just now'
      : `forge polled ${forgePollAge} ago`
    : ''
  
  return (
    <span
      ref={box}
      className="daemon-status"
      onMouseEnter={() => setOpen(true)}
      onMouseLeave={() => !pinned && setOpen(false)}
      onBlur={(e) => !pinned && !e.currentTarget.contains(e.relatedTarget as Node | null) && setOpen(false)}
    >
      <button
        type="button"
        className={`live-dot ${down ? 'is-down' : ready ? 'is-up' : ''}`}
        aria-label={`${statusText}${forgeText ? `, ${forgeText}` : ''}`}
        aria-describedby={id}
        aria-expanded={open}
        onFocus={() => setOpen(true)}
        onClick={() => {
          setPinned(!(open && pinned))
          setOpen(!(open && pinned))
        }}
      >
        <span aria-hidden="true" className="daemon-dot-indicator">{down ? '○' : '●'}</span>
        <span className="daemon-status-text">{statusText}{forgeText}</span>
        <span className="visually-hidden">{down ? 'Not connected' : 'Connected'}</span>
      </button>
      <div id={id} role="tooltip" className="daemon-pop" hidden={!open}>
        {open && <Body down={down} error={error} downSince={downSince} lastEvent={lastEvent} data={data} failed={failed} now={now} />}
      </div>
    </span>
  )
}

function Body({ down, error, downSince, lastEvent, data, failed, now }: {
  down: boolean
  error: string | null
  downSince: number | null
  lastEvent?: string
  data: Loaded | null
  failed: string | null
  now: number
}) {
  const d = data?.status.detail
  const s = data?.session
  const lost = downSince !== null ? `${clockTime(new Date(downSince).toISOString(), now)} (${age(now - downSince)} ago)` : 'a moment ago'
  return (
    <>
      {down && (
        <p className="daemon-offline">
          <strong>Offline</strong> since {lost}. Retrying.
          {error && <span className="faint"> {error}</span>}
        </p>
      )}
      {!down && failed && <p className="daemon-offline">Could not read the daemon's detail: {failed}</p>}
      {!down && !d && !failed && <p className="faint">Loading…</p>}
      {!down && data && d && (
        <>
          <Group title="Daemon">
            <Row k="Version">
              {data.status.version}
              {d.commit && <span className="faint"> ({shortSha(d.commit)})</span>}
            </Row>
            {d.built_at && <Row k="Built">{clockTime(d.built_at, now)}</Row>}
            <Row k="Uptime">
              {age(Date.parse(d.now) - Date.parse(data.status.started))}
              <span className="faint"> since {clockTime(data.status.started, now)}</span>
            </Row>
            <Row k="System">
              {d.os}/{d.arch}
              {d.host && <span className="faint"> on {d.host}</span>}
            </Row>
          </Group>
          <Group title="Config">
            <Row k="Path"><code>{data.status.config}</code></Row>
            <Row k="Loaded">
              {clockTime(d.config_loaded, now)}
              <span className="faint"> ({since(d.config_loaded, now) || 'now'} ago)</span>
            </Row>
            {d.reload_error && (
              <Row k="Reload">
                <span className="tone-bad">failed{d.reload_failed_at && ` ${clockTime(d.reload_failed_at, now)}`}: {d.reload_error}</span>
              </Row>
            )}
          </Group>
          <Group title="Activity">
            <Row k="Agents">{d.running} running</Row>
            <Row k="Lanes">{d.open_lanes} open</Row>
            <Row k="Decisions">
              {d.decisions > 0 ? <span className="tone-warn">{d.decisions} waiting</span> : 'none waiting'}
            </Row>
            <Row k="Quota holds">{quota(d, now)}</Row>
          </Group>
          <Group title="Spend today">
            <Row k="Spent">
              {usd(d.spend_usd)}
              {d.budget_usd ? <span className="faint"> of {usd(d.budget_usd)}</span> : <span className="faint"> (no daily budget)</span>}
            </Row>
          </Group>
          <Group title="Forge watcher">
            {!d.forge && <Row k="Poll"><span className="faint">not reported</span></Row>}
            {d.forge?.error ? (
              <Row k="Error"><span className="tone-bad">{d.forge.error}</span></Row>
            ) : (
              d.forge && (
                <Row k="Last poll">{d.forge.last_poll ? `${since(d.forge.last_poll, now) || 'now'} ago` : 'not yet'}</Row>
              )
            )}
          </Group>
        </>
      )}
      <Group title="This browser">
        <Row k="Stream">{down ? 'not connected' : 'connected'}</Row>
        <Row k="Last event">{lastEvent ? `${since(lastEvent, now) || 'now'} ago` : 'none yet'}</Row>
        {s && (
          <>
            <Row k="Settings">{s.allow_settings ? 'may save' : 'read only'}</Row>
            {s.expires && <Row k="Session ends">{clockTime(s.expires, now)}</Row>}
          </>
        )}
      </Group>
    </>
  )
}

function quota(d: StatusDetail, now: number): ReactNode {
  if (!d.quota_holds) return <span className="faint">not reported</span>
  if (d.quota_holds.length === 0) return 'none'
  return d.quota_holds.map((h) => `${h.agent} until ${clockTime(h.until, now)}`).join(', ')
}

function Group({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="daemon-group">
      <h3>{title}</h3>
      <dl>{children}</dl>
    </section>
  )
}

function Row({ k, children }: { k: string; children: ReactNode }) {
  return (
    <div>
      <dt>{k}</dt>
      <dd>{children}</dd>
    </div>
  )
}
