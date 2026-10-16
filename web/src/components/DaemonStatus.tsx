import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import { daemonInfo } from '../api/client'
import type { SpendToday, Status, StatusDetail, WebSession } from '../api/types'
import { age, clockTime, shortSha, since, usd } from '../format'
import { useNow } from '../hooks'
import { href } from '../router'
import { chatWords, worstTone, type ChatLink } from '../shell/chatLink'
import { RailSpend, spendWords } from '../shell/RailSpend'
import type { StatusCounts } from '../state/attention'

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

/** Reads the daemon's detail while `active`, and again every REFRESH_MS. */
export function useDaemonDetail(active: boolean, load: () => Promise<Loaded>) {
  const [data, setData] = useState<Loaded | null>(null)
  const [failed, setFailed] = useState<string | null>(null)
  useEffect(() => {
    if (!active) return
    let live = true
    const read = () =>
      load().then(
        (d) => live && (setData(d), setFailed(null)),
        (e: unknown) => live && setFailed(e instanceof Error ? e.message : String(e)),
      )
    void read()
    const t = window.setInterval(() => void read(), REFRESH_MS)
    return () => {
      live = false
      window.clearInterval(t)
    }
  }, [active, load])
  return { data, failed }
}

interface Props {
  /** The live connection: why it is down (null when up), since when, and the chat stream. */
  error: string | null
  downSince: number | null
  ready: boolean
  /** The open chat's /stream connection; null when no chat is open. */
  chat?: ChatLink | null
  /** Today's spend, shown as a compact line under the status and in full in the popover. */
  spend?: SpendToday | null
  /** Lane counts, spelled out in the popover. */
  counts?: StatusCounts
  load?: () => Promise<Loaded>
  /** The time now in ms; the clock ticks by itself when omitted. */
  now?: number
}

/**
 * The rail's one status dot: green when the daemon link and the chat stream are both up,
 * amber while the chat stream catches up, red when the daemon is unreachable or the chat is
 * signed out or refused. Hovering, focusing or tapping it opens a short popover; Escape
 * closes it. The detail is read only while it is open. "Details" opens the Status page.
 */
export function DaemonStatus({ error, downSince, ready, chat = null, spend = null, counts, load = loadDaemon, now: nowProp }: Props) {
  const ticking = useNow(10_000)
  const now = nowProp ?? ticking
  const id = useId()
  const [open, setOpen] = useState(false)
  const [pinned, setPinned] = useState(false)
  const box = useRef<HTMLSpanElement>(null)
  const pop = useRef<HTMLDivElement>(null)
  const [place, setPlace] = useState<CSSProperties>({})
  const { data, failed } = useDaemonDetail(open, load)

  const close = useCallback(() => {
    setOpen(false)
    setPinned(false)
  }, [])

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && close()
    const onDown = (e: Event) => box.current && !box.current.contains(e.target as Node) && close()
    document.addEventListener('keydown', onKey)
    document.addEventListener('pointerdown', onDown)
    return () => {
      document.removeEventListener('keydown', onKey)
      document.removeEventListener('pointerdown', onDown)
    }
  }, [open, close])

  // The sidebar scrolls and clips, so the popover is fixed: beside the sidebar when there is
  // room, else above the status line inside the sidebar's width.
  useLayoutEffect(() => {
    if (!open || !box.current) return
    const b = box.current.getBoundingClientRect()
    const side = box.current.closest('.sidebar')?.getBoundingClientRect()
    const wide = pop.current?.offsetWidth ?? 0
    const edge = side ? side.right : b.right
    if (window.innerWidth - edge >= wide + 16) setPlace({ left: edge + 8, bottom: Math.max(8, window.innerHeight - b.bottom) })
    else setPlace({ left: (side?.left ?? b.left) + 8, bottom: window.innerHeight - b.top + 6, maxWidth: (side?.width ?? window.innerWidth) - 16 })
  }, [open])

  const tone = worstTone({ error, ready }, chat)
  const down = error !== null
  const statusText =
    tone === 'up' ? 'Daemon live'
    : tone === 'warn' ? `Chat ${chatWords(chat)}`
    : tone === 'pending' ? 'Daemon connecting'
    : down ? 'Daemon down'
    : `Chat ${chatWords(chat)}`
  // A held snapshot is not current once the daemon is down or a refresh has failed.
  const fresh = !down && !failed ? data : null
  const d = fresh?.status.detail
  const poll = d?.forge?.last_poll
  const unknown = <span className="faint">{down || failed ? 'unknown' : '…'}</span>

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
        className={`live-dot is-${tone}`}
        aria-label={statusText}
        title={statusText}
        aria-haspopup="dialog"
        aria-controls={id}
        aria-expanded={open}
        onFocus={() => setOpen(true)}
        onClick={() => {
          setPinned(!(open && pinned))
          setOpen(!(open && pinned))
        }}
      >
        <span aria-hidden="true" className="daemon-dot-indicator">{tone === 'up' ? '●' : tone === 'warn' ? '◐' : '○'}</span>
        <span className="daemon-status-text">{statusText}</span>
      </button>
      {spend && <RailSpend spend={spend} />}
      <div id={id} ref={pop} role="dialog" aria-label="Status" className="daemon-pop" hidden={!open} style={place}>
        {open && (
          <>
            <dl className="daemon-short">
              <Row k="Daemon">
                {down ? (
                  <span className="tone-bad">down{downSince !== null && ` for ${age(now - downSince)}`}</span>
                ) : failed ? (
                  <span className="tone-bad">unreadable</span>
                ) : d && data ? (
                  <>up <span className="faint">{age(Date.parse(d.now) - Date.parse(data.status.started))}</span></>
                ) : (
                  <span className="faint">…</span>
                )}
              </Row>
              <Row k="Chat">{chatWords(chat)}</Row>
              <Row k="Forge">{d ? (d.forge?.error ? <span className="tone-bad">error</span> : poll ? `polled ${since(poll, now) || 'just now'}${since(poll, now) ? ' ago' : ''}` : 'not polled yet') : unknown}</Row>
              <Row k="Version">{fresh ? fresh.status.version : unknown}</Row>
              {spend && <Row k="Spend">{spendWords(spend)}</Row>}
              {counts && (
                <Row k="Lanes">
                  {counts.running} running, {counts.waiting} waiting, {counts.broken} broken, {counts.routing} to route
                </Row>
              )}
            </dl>
            <a className="daemon-details" href={href.status()} onClick={close}>Details →</a>
          </>
        )}
      </div>
    </span>
  )
}

/** Everything the daemon reports, grouped; the Status page's body. */
export function DaemonDetail({ down, error, downSince, lastEvent, chat, data, failed, now }: {
  down: boolean
  error: string | null
  downSince: number | null
  lastEvent?: string
  chat: ChatLink | null
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
        <Row k="Event stream">{down ? 'not connected' : 'connected'}</Row>
        <Row k="Chat stream">{chatWords(chat)}</Row>
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
