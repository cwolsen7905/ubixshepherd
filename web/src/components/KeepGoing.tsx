import { useCallback, useEffect, useState } from 'react'
import { get, post } from '../api/client'
import { usd } from '../format'
import { cx, Toggle } from '../ui'

/** What `GET /v1/keep-going` answers (internal/api/keepgoing.go), the fields the switch reads. */
export interface KeepGoingStatus {
  on: boolean
  reason_off?: string
  off_at?: string
  spent_usd: number
  cap_usd: number
  runs_now: number
  max_runs: number
}

const PATH = '/v1/keep-going'

/** How often the switch re-reads, since the daemon can turn the mode off itself at a limit. */
export const REFRESH_MS = 30 * 1000
/** A stop is worth a line for this long. */
const RECENT_OFF_MS = 60 * 60 * 1000

const WHY: Record<string, string> = {
  cap: 'spend cap reached',
  budget: 'daily budget reached',
  idle: 'idle too long',
  day_end: 'day ended',
}

interface Props {
  load?: () => Promise<KeepGoingStatus>
  set?: (on: boolean) => Promise<KeepGoingStatus>
  now?: () => number
}

const reason = (e: unknown) => (e instanceof Error ? e.message : String(e))

/** The one line under the switch: spend against the cap and runs while on, why it stopped after. */
export function detail(st: KeepGoingStatus, now: number): { text: string; warn: boolean } | null {
  if (st.on) {
    const parts: string[] = []
    let warn = false
    if (st.cap_usd > 0) {
      parts.push(`${usd(st.spent_usd) || '$0.00'} of ${usd(st.cap_usd)}`)
      warn = st.spent_usd >= 0.8 * st.cap_usd
    }
    if (st.max_runs > 0) parts.push(`${st.runs_now} of ${st.max_runs} runs`)
    return parts.length ? { text: parts.join(', '), warn } : null
  }
  const why = st.reason_off ? WHY[st.reason_off] : undefined
  const at = st.off_at ? Date.parse(st.off_at) : NaN
  if (why && Number.isFinite(at) && now - at >= 0 && now - at < RECENT_OFF_MS) return { text: `off: ${why}`, warn: false }
  return null
}

/**
 * The Keep going switch in the sidebar footer. Turning it on is the person's click and
 * nothing else: it never turns itself on, and the daemon refuses it from an agent. Off is
 * one click. A failed read shows nothing (an older daemon has no such route); a refused or
 * failed change says why and leaves the switch where the daemon last said it was.
 */
export function KeepGoing({
  load = () => get<KeepGoingStatus>(PATH),
  set = (on) => post<KeepGoingStatus>(PATH, { on }),
  now = Date.now,
}: Props) {
  const [st, setSt] = useState<KeepGoingStatus | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)

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

  const toggle = useCallback(() => {
    if (!st || busy) return
    setBusy(true)
    setErr(null)
    set(!st.on)
      .then(setSt)
      .catch((e: unknown) => setErr(reason(e)))
      .finally(() => setBusy(false))
  }, [st, busy, set])

  if (!st) return null
  const d = detail(st, now())
  const capped = st.on && st.cap_usd > 0
  const pct = capped ? Math.min(100, (st.spent_usd / st.cap_usd) * 100) : st.on ? 100 : 0
  const fill = { '--kg-fill-pct': `${pct}%` } as React.CSSProperties
  const barClass = cx('sh-keep-bar', st.on && 'is-on', capped && d?.warn && 'tone-warn')
  return (
    <div className="sh-keep" data-component="KeepGoing">
      <div className="sh-keep-row">
        <span>Keep going</span>
        <Toggle
          checked={st.on}
          label="Keep going"
          disabled={busy}
          title="Let the front desk carry on with approved work without waiting between steps"
          onChange={toggle}
        />
      </div>
      {capped && d ? (
        <span
          className={barClass}
          style={fill}
          role="progressbar"
          aria-valuemin={0}
          aria-valuemax={Math.round(st.cap_usd * 100) / 100}
          aria-valuenow={Math.round(Math.min(st.spent_usd, st.cap_usd) * 100) / 100}
          aria-valuetext={d.text}
          aria-label="Keep going spend"
        >
          <b />
        </span>
      ) : (
        <span className={barClass} style={fill} aria-hidden="true">
          <b />
        </span>
      )}
      {d && <span className={cx('sh-keep-note', d.warn && 'is-near', d.warn && 'tone-warn')}>{d.text}</span>}
      {err && (
        <span className="sh-keep-note is-near" role="alert">
          {err}
        </span>
      )}
    </div>
  )
}
