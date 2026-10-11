import { useLayoutEffect, useRef } from 'react'
import { clockTime, duration, runCost, usd } from '../format'
import {
  agentClass, runPhrase, type DecisionMark, type MergeMark, type SpendStep, type TRun,
} from '../model/timeline'

/** What the pointer or focus is on. */
export type Tip =
  | { kind: 'run'; run: TRun; lane: string; repo: string; continuing: boolean }
  | { kind: 'density'; runs: TRun[]; failed: number; lane: string }
  | { kind: 'decision'; mark: DecisionMark; lane: string }
  | { kind: 'merge'; mark: MergeMark; lane: string }
  | { kind: 'spend'; at: number; cum: number; step?: SpendStep; budget: number }

/** A tip and where to put it, in viewport coordinates. */
export interface TipAt {
  tip: Tip
  x: number
  y: number
}

const iso = (ms: number) => new Date(ms).toISOString()

function Row({ value, label }: { value: string; label: string }) {
  return (
    <div className="tl-tip-row">
      <span className="tl-tip-value">{value}</span>
      <span className="tl-tip-label">{label}</span>
    </div>
  )
}

function RunTip({ tip, now }: { tip: Extract<Tip, { kind: 'run' }>; now: number }) {
  const r = tip.run
  const end = r.ended !== undefined ? clockTime(iso(r.ended), now) : 'now'
  return (
    <>
      <p className="tl-tip-head">
        <span className={`tl-key tl-agent-${agentClass(r.agent)}`} aria-hidden="true" />
        <strong>{r.agent}</strong>
        {r.model && <span className="tl-tip-model">{r.model}</span>}
      </p>
      <Row value={runPhrase(r, now)} label={r.state === 'failed' ? '✗ failed' : 'state'} />
      <Row value={`${clockTime(iso(r.started), now)} to ${end}`} label={tip.continuing ? 'began before this range' : 'start to end'} />
      <Row value={runCost({ cost_usd: r.costUsd, credits: r.credits }) || 'none yet'} label={r.state === 'running' ? 'cost, when it ends' : 'cost'} />
      <Row value={String(r.commits)} label={r.commits === 1 ? 'commit' : 'commits'} />
      {r.progress && <p className="tl-tip-line">{r.progress}</p>}
      {r.diagnosis && <p className="tl-tip-line tl-tip-bad">{r.diagnosis}</p>}
      {r.task && <p className="tl-tip-task">{r.task}</p>}
      <p className="tl-tip-where">
        {tip.repo} / {tip.lane}
      </p>
    </>
  )
}

function DecisionTip({ tip, now }: { tip: Extract<Tip, { kind: 'decision' }>; now: number }) {
  const m = tip.mark
  const asked = clockTime(iso(m.created), now)
  const what = m.source === 'request' ? 'request, needs routing' : m.open ? 'decision, waiting on you' : 'decision, answered'
  return (
    <>
      <p className="tl-tip-head">
        <strong>{what}</strong>
      </p>
      <p className="tl-tip-task">{m.question}</p>
      {m.open ? (
        <Row value={duration(iso(m.created), undefined, now)} label={`waiting since ${asked}`} />
      ) : (
        <Row value={duration(iso(m.created), iso(m.answered ?? m.created), now)} label={`answered, asked ${asked}`} />
      )}
      {m.recommendation && <Row value={m.recommendation} label="recommended" />}
      <p className="tl-tip-where">{tip.lane}</p>
    </>
  )
}

function SpendTip({ tip, now }: { tip: Extract<Tip, { kind: 'spend' }>; now: number }) {
  const s = tip.step
  return (
    <>
      <p className="tl-tip-head">
        <strong>Spend</strong>
      </p>
      <Row value={usd(tip.cum)} label={`so far that day, at ${clockTime(iso(tip.at), now)}`} />
      {tip.budget > 0 && <Row value={usd(tip.budget)} label="daily budget" />}
      {s && (
        <Row
          value={`+${usd(s.usd)}`}
          label={s.source === 'run' ? `run ${s.run}, at ${clockTime(iso(s.at), now)}` : 'the front desk and spend with no time of its own'}
        />
      )}
      <p className="tl-tip-line">A running run adds its cost when it ends.</p>
    </>
  )
}

/** One tooltip for the whole chart: shown on hover and on keyboard focus, flipped at the edges of its container. */
export function TimelineTooltip({ at, now, boundary }: { at: TipAt | null; now: number; boundary: HTMLElement | null }) {
  const ref = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el || !at || !boundary) return
    const box = boundary.getBoundingClientRect()
    const w = el.offsetWidth
    const h = el.offsetHeight
    let left = at.x - box.left + 14
    let top = at.y - box.top + 16
    // Flip to the other side of the pointer rather than run off the edge.
    if (left + w > box.width) left = Math.max(0, at.x - box.left - w - 14)
    if (top + h > box.height && at.y - box.top - h - 12 > 0) top = at.y - box.top - h - 12
    el.style.left = `${Math.round(left)}px`
    el.style.top = `${Math.round(top)}px`
  })
  if (!at) return null
  const t = at.tip
  return (
    <div ref={ref} className="tl-tip" role="tooltip">
      {t.kind === 'run' && <RunTip tip={t} now={now} />}
      {t.kind === 'decision' && <DecisionTip tip={t} now={now} />}
      {t.kind === 'spend' && <SpendTip tip={t} now={now} />}
      {t.kind === 'merge' && (
        <>
          <p className="tl-tip-head">
            <strong>{t.mark.mr ? `!${t.mark.mr} merged` : 'merged'}</strong>
          </p>
          <Row value={`about ${clockTime(iso(t.mark.at), now)}`} label="as of the daemon’s last look at the forge" />
          <p className="tl-tip-where">{t.lane}</p>
        </>
      )}
      {t.kind === 'density' && (
        <>
          <p className="tl-tip-head">
            <strong>{t.runs.length} runs</strong>
          </p>
          <Row value={`${clockTime(iso(Math.min(...t.runs.map((r) => r.started))), now)} on`} label="too close together to draw one by one" />
          {t.failed > 0 && <Row value={String(t.failed)} label="failed" />}
          <p className="tl-tip-where">{t.lane}</p>
        </>
      )}
    </div>
  )
}
