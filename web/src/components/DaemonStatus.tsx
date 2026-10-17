import { useCallback, useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { daemonInfo } from '../api/client'
import type { SpendToday, Status, StatusDetail, WebSession } from '../api/types'
import { age, clockTime, shortSha, since, usd } from '../format'
import { useNow } from '../hooks'
import { href } from '../router'
import { chatWords, worstTone, type ChatLink } from '../shell/chatLink'
import { spendWords } from '../shell/RailSpend'
import type { StatusCounts } from '../state/attention'
import { offlineWords, retryIn } from '../ui'

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

/** How often the closed status line reads the forge's last poll. */
export const WATCH_MS = 30_000

/** Reads the daemon's detail while `active`, and again every `every` ms (REFRESH_MS by default). */
export function useDaemonDetail(active: boolean, load: () => Promise<Loaded>, every = REFRESH_MS) {
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
    const t = window.setInterval(() => void read(), every)
    return () => {
      live = false
      window.clearInterval(t)
    }
  }, [active, load, every])
  return { data, failed }
}

interface Props {
  /** The live connection: why it is down (null when up), since when, and the chat stream. */
  error: string | null
  downSince: number | null
  ready: boolean
  /** The open chat's /stream connection; null when no chat is open. */
  chat?: ChatLink | null
  /** Today's spend, spelled out in the popover (the footer draws its meter above the status line). */
  spend?: SpendToday | null
  /** Lane counts, spelled out in the popover. */
  counts?: StatusCounts
  load?: () => Promise<Loaded>
  /** Read the forge's last poll while the popover is closed, so the line can say "forge 41s ago". */
  watch?: boolean
  /** The time now in ms; the clock ticks by itself when omitted. */
  now?: number
  /** While the daemon drains for a restart: the words for the Daemon row. */
  restart?: string | null
}

/** How long the popover stays after the pointer leaves, so the pointer can reach it. */
export const CLOSE_MS = 150

/**
 * The sidebar's status line (`.sh-daemon`): a dot and "Online, forge 41s ago" or "Offline,
 * retrying in 8s". Hovering, focusing or tapping it opens the status popover beside the sidebar
 * (a short delay on leaving lets the pointer reach it); Escape closes it. The detail is read
 * only while it is open. "Details" opens the Status page. The dot is the worse of the daemon
 * link and the chat stream, and never stands without the words.
 */
export function DaemonStatus({ error, downSince, ready, chat = null, spend = null, counts, load = loadDaemon, watch = false, now: nowProp, restart = null }: Props) {
  const down = error !== null
  const ticking = useNow(down ? 1000 : 10_000)
  const now = nowProp ?? ticking
  const id = useId()
  const [open, setOpen] = useState(false)
  const [pinned, setPinned] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  const leave = useRef<number | undefined>(undefined)
  const { data, failed } = useDaemonDetail(open || watch, load, open ? REFRESH_MS : WATCH_MS)

  const close = useCallback(() => {
    window.clearTimeout(leave.current)
    setOpen(false)
    setPinned(false)
  }, [])
  const enter = useCallback(() => {
    window.clearTimeout(leave.current)
    setOpen(true)
  }, [])
  useEffect(() => () => window.clearTimeout(leave.current), [])

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

  const tone = worstTone({ error, ready }, chat)
  // A held snapshot is not current once the daemon is down or a refresh has failed.
  const fresh = !down && !failed ? data : null
  const d = fresh?.status.detail
  const poll = d?.forge?.last_poll
  const forgeAge = poll ? since(poll, now) : ''
  const forgeWords = d?.forge?.error ? ', forge error' : poll ? `, forge ${forgeAge ? `${forgeAge} ago` : 'just now'}` : ''
  const statusText =
    down ? offlineWords({ offline: true, retryIn: retryIn(downSince, now) })
    : tone === 'up' ? `Online${forgeWords}`
    : tone === 'warn' ? `Online, chat ${chatWords(chat)}`
    : tone === 'pending' ? 'Connecting'
    : `Chat ${chatWords(chat)}`
  const unknown = <span className="is-faint">{down || failed ? 'unknown' : '…'}</span>

  return (
    <div
      ref={box}
      className="sh-daemon"
      data-component="StatusPopover"
      onMouseEnter={enter}
      onMouseLeave={() => {
        if (pinned) return
        window.clearTimeout(leave.current)
        leave.current = window.setTimeout(() => setOpen(false), CLOSE_MS)
      }}
      onBlur={(e) => !pinned && !e.currentTarget.contains(e.relatedTarget as Node | null) && setOpen(false)}
    >
      <button
        type="button"
        className="sh-daemon-line"
        aria-label={statusText}
        title={statusText}
        aria-haspopup="dialog"
        aria-controls={id}
        aria-expanded={open}
        onFocus={enter}
        onClick={() => {
          setPinned(!(open && pinned))
          setOpen(!(open && pinned))
        }}
      >
        <span aria-hidden="true" className={`sh-daemon-dot is-${tone}`}>{tone === 'up' ? '●' : tone === 'warn' ? '◐' : '○'}</span>
        <span>{statusText}</span>
      </button>
      <div id={id} role="dialog" aria-label="Status" className="sh-status-pop" hidden={!open}>
        {open && (
          <div className="sh-status-pop-card">
            <dl className="sh-dl">
              <PopRow k="Daemon">
                {restart ? (
                  <>{restart}</>
                ) : down ? (
                  <span className="is-bad">down{downSince !== null && ` for ${age(now - downSince)}`}</span>
                ) : failed ? (
                  <span className="is-bad">unreadable</span>
                ) : d && data ? (
                  <>up <span className="is-faint">{age(Date.parse(d.now) - Date.parse(data.status.started))}</span></>
                ) : (
                  <span className="is-faint">…</span>
                )}
              </PopRow>
              <PopRow k="Chat">{chatWords(chat)}</PopRow>
              <PopRow k="Forge">{d ? (d.forge?.error ? <span className="is-bad">error</span> : poll ? `polled ${since(poll, now) || 'just now'}${since(poll, now) ? ' ago' : ''}` : 'not polled yet') : unknown}</PopRow>
              <PopRow k="Version">{fresh ? fresh.status.version : unknown}</PopRow>
              {spend && <PopRow k="Spend">{spendWords(spend)}</PopRow>}
              {counts && (
                <PopRow k="Lanes">
                  {counts.running} running, {counts.waiting} waiting, {counts.broken} broken, {counts.routing} to route
                </PopRow>
              )}
            </dl>
            <a className="sh-details" href={href.status()} onClick={close}>Details →</a>
          </div>
        )}
      </div>
    </div>
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

/** A row of the popover's `.sh-dl`: the term and its value as direct grid cells. */
function PopRow({ k, children }: { k: string; children: ReactNode }) {
  return (
    <>
      <dt>{k}</dt>
      <dd>{children}</dd>
    </>
  )
}
