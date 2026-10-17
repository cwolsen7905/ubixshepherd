// The swarm timeline's chart: a track per lane, grouped by repo, a bar per run, the markers
// and the spend band. Hand-written SVG; the geometry comes from model/timeline.ts and this
// file only draws it. Running bars and the now line are moved by one timer that sets
// attributes through the DOM once a second, so React does not render on that tick.
import { memo, useCallback, useEffect, useMemo, useRef, useState, type FocusEvent as ReactFocusEvent, type PointerEvent as ReactPointerEvent, type ReactNode } from 'react'
import { duration } from '../format'
import {
  agentClass, BAR_REM, CAP_REM, layoutTimeline, quantumFor, ROW_REM, runLabel, spendAt, stabilise, ticks, timeAt, timeOfDay, windowFor,
  xOf, type BarMark, type DecisionMark, type DensityMark, type LaneTrack, type MergeMark, type RepoGroup, type SpendBand, type TimelineData,
  type TimelineLayout,
} from '../model/timeline'
import { href, type TimelineRange } from '../router'
import { openLink, useHashOpen } from './BacklogOpen'
import { TimelineTooltip, type Tip, type TipAt } from './TimelineTooltip'
import '../timeline/timeline.css'

type OnTip = (tip: Tip | null, x?: number, y?: number) => void

const COLLAPSED_KEY = 'shepherd.timeline.collapsed'

function readCollapsed(): Set<string> {
  try {
    const v = JSON.parse(window.localStorage.getItem(COLLAPSED_KEY) ?? '[]') as unknown
    return new Set(Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [])
  } catch {
    return new Set()
  }
}

function writeCollapsed(set: Set<string>) {
  try {
    window.localStorage.setItem(COLLAPSED_KEY, JSON.stringify([...set]))
  } catch {
    // no storage: the choice lasts until the page reloads
  }
}

/** A media query that the browser answers, with its changes; false where there is no matchMedia (tests). */
function useMedia(query: string): boolean {
  const [on, setOn] = useState(() => typeof window.matchMedia === 'function' && window.matchMedia(query).matches)
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return
    const mq = window.matchMedia(query)
    const change = () => setOn(mq.matches)
    change()
    mq.addEventListener?.('change', change)
    return () => mq.removeEventListener?.('change', change)
  }, [query])
  return on
}

/** The person has asked for less motion: the pulse and the breathing give way to static markers. */
export const useReducedMotion = () => useMedia('(prefers-reduced-motion: reduce)')

/** The narrow layout of the app (its one breakpoint). */
export const useNarrow = () => useMedia('(max-width: 760px)')

/** The root font size in px: the geometry is built from rems, and SVG wants numbers. */
function rootRem(): number {
  const v = parseFloat(window.getComputedStyle(document.documentElement).fontSize)
  return Number.isFinite(v) && v > 0 ? v : 16
}

/** The hover and focus handlers of a mark, all setting the same tooltip. */
function tipHandlers(tip: Tip, onTip: OnTip) {
  return {
    onPointerEnter: (e: ReactPointerEvent) => onTip(tip, e.clientX, e.clientY),
    onPointerMove: (e: ReactPointerEvent) => onTip(tip, e.clientX, e.clientY),
    onPointerLeave: () => onTip(null),
    onFocus: (e: ReactFocusEvent<Element>) => {
      const r = e.currentTarget.getBoundingClientRect()
      onTip(tip, r.left + r.width / 2, r.bottom)
    },
    onBlur: () => onTip(null),
  }
}

interface Geometry {
  rowH: number
  barH: number
  barY: number
  rem: number
}

function geometry(rem: number): Geometry {
  const rowH = Math.round(ROW_REM * rem)
  const barH = Math.round(BAR_REM * rem)
  return { rowH, barH, barY: Math.round((rowH - barH) / 2), rem }
}

/** The event mark at a finished bar's end: ⌀ out of quota and ✗ failed on a bar of any width (a failure is always shown), ✓ passed only on a bar wide enough to be labelled. */
function endGlyph(mark: BarMark): { ch: string; cls: string } | null {
  const r = mark.run
  if (mark.running) return null
  if (mark.failed) return /quota/i.test(r.diagnosis ?? '') ? { ch: '⌀', cls: 'tl-fail-glyph' } : { ch: '✗', cls: 'tl-fail-glyph' }
  if (r.state === 'succeeded' && mark.labelled) return { ch: '✓', cls: 'tl-ok-glyph' }
  return null
}

// --- Marks ------------------------------------------------------------------------

const Bar = memo(function Bar({
  mark, lane, repo, g, now, dim, plotW, onTip,
}: { mark: BarMark; lane: string; repo: string; g: Geometry; now: number; dim: boolean; plotW: number; onTip: OnTip }) {
  useHashOpen()
  const r = mark.run
  const cls = agentClass(r.agent)
  const tip: Tip = { kind: 'run', run: r, lane, repo, continuing: mark.continuing }
  const labelPad = 0.5 * g.rem
  const cap = CAP_REM * g.rem
  const fade = 1.6 * g.rem
  const end = mark.x + mark.w
  const notch = Math.round(g.barH * 0.5)
  const tail = endGlyph(mark)
  const tailIn = !!tail && end + 3 + g.rem > plotW
  const classes = [
    'tl-run', `tl-agent-${cls}`, mark.running && 'is-running', mark.failed && 'is-failed', mark.continuing && 'is-continuing', dim && 'is-dim',
  ].filter(Boolean).join(' ')
  return (
    <a
      {...openLink({ kind: 'run', id: r.id })}
      className={classes}
      role="link"
      aria-label={runLabel(r, lane, now)}
      data-run={r.id}
      {...tipHandlers(tip, onTip)}
    >
      <rect className="tl-bar" x={mark.x} y={g.barY} width={mark.w} height={g.barH} rx={3} data-live={mark.running ? 'bar' : undefined} data-x0={mark.x} />
      {mark.labelled && (
        <svg x={mark.x} y={g.barY} width={Math.max(0, mark.w)} height={g.barH} className="tl-label-clip" data-live={mark.running ? 'clip' : undefined} data-x0={mark.x}>
          <text className="tl-bar-label" x={labelPad} y={g.barH / 2} dominantBaseline="central">
            {r.agent}
          </text>
        </svg>
      )}
      {mark.running && (
        <>
          <rect className="tl-fade" x={Math.max(mark.x, end - fade)} y={g.barY} width={Math.min(fade, mark.w)} height={g.barH} data-live="fade" data-x0={mark.x} data-w={fade} />
          <rect className="tl-cap" x={Math.max(mark.x, end - cap)} y={g.barY} width={Math.min(cap, mark.w)} height={g.barH} rx={3} data-live="cap" data-x0={mark.x} data-w={cap} />
        </>
      )}
      {mark.failed && (
        <path
          className="tl-notch"
          d={`M${end} ${g.barY}L${end - notch} ${g.barY + g.barH / 2}L${end} ${g.barY + g.barH}Z`}
        />
      )}
      {tail && (
        // Past the plot's right edge a glyph would be clipped: it is drawn at the edge instead, in its own colour on its halo.
        <text
          className={`tl-glyph ${tail.cls}`}
          x={tailIn ? plotW - 2 : end + 3}
          y={g.rowH / 2}
          textAnchor={tailIn ? 'end' : 'start'}
          dominantBaseline="central"
          aria-hidden="true"
        >
          {tail.ch}
        </text>
      )}
      {/* A bigger target than the bar, so a thin one is still easy to reach. */}
      <rect className="tl-hit" x={mark.x} y={0} width={Math.max(mark.w, 6)} height={g.rowH} />
    </a>
  )
})

const Density = memo(function Density({
  mark, lane, laneId, g, dim, onTip,
}: { mark: DensityMark; lane: string; laneId: number; g: Geometry; dim: boolean; onTip: OnTip }) {
  const tip: Tip = { kind: 'density', runs: mark.runs, failed: mark.failed, lane }
  return (
    <a
      href={href.runs({ lane: laneId })}
      className={`tl-density${mark.failed > 0 ? ' is-failed' : ''}${dim ? ' is-dim' : ''}`}
      role="link"
      aria-label={`${mark.runs.length} runs in ${lane}${mark.failed > 0 ? `, ${mark.failed} failed` : ''}`}
      {...tipHandlers(tip, onTip)}
    >
      <rect className="tl-density-tick" x={mark.x} y={g.barY} width={mark.w} height={g.barH} />
      <rect className="tl-hit" x={mark.x - 2} y={0} width={mark.w + 4} height={g.rowH} />
    </a>
  )
})

const Ask = memo(function Ask({ mark, lane, g, now, onTip }: { mark: DecisionMark; lane: string; g: Geometry; now: number; onTip: OnTip }) {
  useHashOpen()
  const cy = g.rowH / 2
  const r = 0.4 * g.rem
  const link = mark.source === 'decision' ? openLink({ kind: 'decision', id: mark.id }) : { href: href.decisions() }
  const tip: Tip = { kind: 'decision', mark, lane }
  if (!mark.open) {
    return (
      <a {...link} className="tl-ask is-answered" role="link" aria-label={`Decision answered: ${mark.question}`} {...tipHandlers(tip, onTip)}>
        <text className="tl-answered" x={mark.x} y={cy} textAnchor="middle" dominantBaseline="central">?</text>
        <rect className="tl-hit" x={mark.x - 7} y={0} width={14} height={g.rowH} />
      </a>
    )
  }
  const what = mark.source === 'request' ? 'Request needing routing' : 'Decision waiting on you'
  return (
    <a
      {...link}
      className={`tl-ask is-open${mark.source === 'request' ? ' is-request' : ''}`}
      role="link"
      aria-label={`${what}, waiting ${duration(new Date(mark.created).toISOString(), undefined, now)}: ${mark.question}`}
      {...tipHandlers(tip, onTip)}
    >
      {mark.joinTo !== undefined && <line className="tl-join" x1={mark.x} x2={mark.joinTo} y1={cy} y2={cy} />}
      <circle className="tl-halo" cx={mark.x} cy={cy} r={r * 2.6} />
      <text className="tl-glyph" x={mark.x} y={cy} textAnchor="middle" dominantBaseline="central">{mark.source === 'request' ? '!' : '?'}</text>
      <rect className="tl-hit" x={mark.x - 9} y={0} width={18} height={g.rowH} />
    </a>
  )
})

const Merge = memo(function Merge({ mark, lane, laneId, g, onTip }: { mark: MergeMark; lane: string; laneId: number; g: Geometry; onTip: OnTip }) {
  useHashOpen()
  const tip: Tip = { kind: 'merge', mark, lane }
  const r = 0.4 * g.rem
  return (
    <a
      {...openLink({ kind: 'lane', id: laneId })}
      className="tl-merge"
      role="link"
      aria-label={`${mark.mr ? `!${mark.mr} merged` : 'Merged'} about ${timeOfDay(mark.at)}, ${lane}`}
      {...tipHandlers(tip, onTip)}
    >
      <line className="tl-merge-tick" x1={mark.x} x2={mark.x} y1={2} y2={g.rowH - 2} />
      <text className="tl-glyph tl-ok-glyph" x={mark.x} y={2 + r} textAnchor="middle" dominantBaseline="central" aria-hidden="true">◆</text>
      <rect className="tl-hit" x={mark.x - 7} y={0} width={14} height={g.rowH} />
    </a>
  )
})

// --- Rows -------------------------------------------------------------------------

const Row = memo(function Row({
  track, width, g, now, agent, onTip,
}: { track: LaneTrack; width: number; g: Geometry; now: number; agent: string; onTip: OnTip }) {
  useHashOpen()
  return (
    <div className={`tl-row${track.waiting ? ' is-waiting' : ''}`} data-lane={track.id}>
      <a className="tl-name" {...openLink({ kind: 'lane', id: track.id })} title={track.name}>
        {track.name}
      </a>
      <svg className="tl-track" width={width} height={g.rowH} aria-label={`${track.name}: ${track.bars.length} marks`} role="group">
        <line className="tl-rail" x1={0} x2={width} y1={g.rowH / 2} y2={g.rowH / 2} />
        {track.bars.map((b) =>
          b.kind === 'bar' ? (
            <Bar key={b.key} mark={b} lane={track.name} repo={track.repo} g={g} now={b.running ? now : 0} dim={!!agent && b.run.agent !== agent} plotW={width} onTip={onTip} />
          ) : (
            <Density key={b.key} mark={b} lane={track.name} laneId={track.id} g={g} dim={!!agent && !b.runs.some((r) => r.agent === agent)} onTip={onTip} />
          ),
        )}
        {track.merges.map((m) => (
          <Merge key={m.key} mark={m} lane={track.name} laneId={track.id} g={g} onTip={onTip} />
        ))}
        {track.decisions.map((d) => (
          <Ask key={d.key} mark={d} lane={track.name} g={g} now={d.open ? now : 0} onTip={onTip} />
        ))}
      </svg>
    </div>
  )
})

function Group({
  group, collapsed, onToggle, ...rest
}: { group: RepoGroup; collapsed: boolean; onToggle: (repo: string) => void; width: number; g: Geometry; now: number; agent: string; onTip: OnTip }) {
  const n = group.lanes.length
  return (
    <section className="tl-group" aria-label={group.repo}>
      <h2 className="tl-group-head">
        <button type="button" aria-expanded={!collapsed} onClick={() => onToggle(group.repo)}>
          <span className="tl-twist" aria-hidden="true">{collapsed ? '▸' : '▾'}</span>
          <span className="tl-group-name">{group.repo}</span>
          <span className="tl-group-count">{n} {n === 1 ? 'lane' : 'lanes'}</span>
        </button>
      </h2>
      {!collapsed && group.lanes.map((t) => <Row key={t.key} track={t} {...rest} />)}
    </section>
  )
}

// --- Spend ------------------------------------------------------------------------

function Spend({
  band, layout, width, onTip,
}: { band: SpendBand; layout: TimelineLayout; width: number; onTip: OnTip }) {
  const [cursor, setCursor] = useState<number | null>(null)
  const w = layout.window
  const move = (e: ReactPointerEvent<SVGRectElement>) => {
    const box = e.currentTarget.getBoundingClientRect()
    const x = e.clientX - box.left
    const t = Math.min(timeAt(w, width, x), w.now)
    setCursor(x)
    const at = spendAt(band, t)
    onTip({ kind: 'spend', at: t, cum: at.cum, ...(at.step ? { step: at.step } : {}), budget: band.budget }, e.clientX, e.clientY)
  }
  return (
    <svg className="tl-spend-svg" width={width} height={band.height} role="img" aria-label={`Spend so far today: ${band.total.toFixed(2)} dollars${band.budget > 0 ? ` of ${band.budget} budget` : ''}`}>
      <path className="tl-spend-fill" d={band.fill} />
      {band.over && <path className="tl-spend-over" d={band.over} />}
      <path className="tl-spend-line" d={band.line} fill="none" />
      {band.budget > 0 && (
        <>
          <line className="tl-budget" x1={0} x2={width} y1={band.budgetY} y2={band.budgetY} />
          <text className="tl-budget-label" x={width - 4} y={Math.max(11, band.budgetY - 4)} textAnchor="end">
            ${Math.round(band.budget)} budget{band.overBudget ? ', over budget' : ''}
          </text>
        </>
      )}
      {cursor !== null && <line className="tl-cursor" x1={cursor} x2={cursor} y1={0} y2={band.height} />}
      <rect
        className="tl-spend-hit"
        x={0}
        y={0}
        width={width}
        height={band.height}
        onPointerMove={move}
        onPointerLeave={() => {
          setCursor(null)
          onTip(null)
        }}
      />
      <line className="tl-now-line tl-now-spend" data-live="now" x1={layout.nowX} x2={layout.nowX} y1={0} y2={band.height} />
    </svg>
  )
}

// --- Chart ------------------------------------------------------------------------

export interface TimelineChartProps {
  data: TimelineData
  range: TimelineRange
  /** The time now, refreshed by the page; the timer moves the running bars between refreshes. */
  now: number
  /** The agent the legend filters to, or "": the others are dimmed. */
  agent: string
  showIdle: boolean
  /** Fixes the plot's width in px; by default the chart measures itself. */
  width?: number
  /** Shown instead of the tracks when nothing ran. */
  empty?: ReactNode
}

const FALLBACK_WIDTH = 960

export function TimelineChart({ data, range, now, agent, showIdle, width: fixed, empty }: TimelineChartProps) {
  const plotRef = useRef<HTMLDivElement>(null)
  const [root, setRoot] = useState<HTMLElement | null>(null)
  const [measured, setMeasured] = useState(0)
  const [rem, setRem] = useState(() => rootRem())
  const [collapsed, setCollapsed] = useState(readCollapsed)
  const [tip, setTip] = useState<TipAt | null>(null)
  const still = useReducedMotion()

  useEffect(() => {
    const el = plotRef.current
    if (!el) return
    const measure = () => {
      setMeasured(Math.floor(el.clientWidth))
      setRem(rootRem())
    }
    measure()
    if (typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const width = fixed ?? (measured > 0 ? measured : FALLBACK_WIDTH)
  const win = useMemo(() => windowFor(range, now, quantumFor(range, now, width)), [range, now, width])
  const raw = useMemo(() => layoutTimeline(data, win, { width, rem }, { showIdle }), [data, win, width, rem, showIdle])
  // Marks that did not change keep their identity, so a new read re-renders only what moved.
  const [seen, setSeen] = useState(raw)
  const [shown, setShown] = useState(raw)
  if (raw !== seen) {
    setSeen(raw)
    setShown(stabilise(shown, raw))
  }

  const g = useMemo(() => geometry(rem), [rem])
  const onTip = useCallback<OnTip>((t, x = 0, y = 0) => setTip(t ? { tip: t, x, y } : null), [])
  const toggle = useCallback((repo: string) => {
    setCollapsed((c) => {
      const next = new Set(c)
      if (!next.delete(repo)) next.add(repo)
      writeCollapsed(next)
      return next
    })
  }, [])

  // The one timer: running bars and the now line, once a second, not while the tab is hidden.
  useEffect(() => {
    if (!root) return
    let lastLabel = ''
    const tick = () => {
      if (document.visibilityState === 'hidden') return
      const t = Date.now()
      const x = Math.round(xOf(shown.window, width, t) * 10) / 10
      for (const el of root.querySelectorAll<SVGElement>('[data-live]')) {
        const x0 = Number(el.getAttribute('data-x0') ?? 0)
        switch (el.getAttribute('data-live')) {
          case 'bar':
          case 'clip':
            el.setAttribute('width', String(Math.max(0, x - x0)))
            break
          case 'fade':
          case 'cap': {
            const w = Math.min(Number(el.getAttribute('data-w') ?? 0), Math.max(0, x - x0))
            el.setAttribute('x', String(x - w))
            el.setAttribute('width', String(w))
            break
          }
          case 'now':
            el.setAttribute('x1', String(x))
            el.setAttribute('x2', String(x))
            break
          case 'now-label': {
            const label = timeOfDay(t)
            el.setAttribute('x', String(x - 4))
            if (label !== lastLabel) {
              el.textContent = label
              lastLabel = label
            }
            break
          }
        }
      }
    }
    const id = window.setInterval(tick, 1000)
    const visible = () => document.visibilityState === 'visible' && tick()
    document.addEventListener('visibilitychange', visible)
    return () => {
      window.clearInterval(id)
      document.removeEventListener('visibilitychange', visible)
    }
  }, [root, shown.window, width])

  const maxTicks = Math.max(2, Math.floor(width / (5.5 * rem)))
  const axis = useMemo(() => ticks(shown.window, maxTicks), [shown.window, maxTicks])
  const axisH = Math.round(1.75 * rem)
  const nowLabel = timeOfDay(now)
  const rows = shown.groups.reduce((n, grp) => n + grp.lanes.length, 0)
  const scrolls = shown.groups.reduce((n, grp) => n + (collapsed.has(grp.repo) ? 0 : grp.lanes.length), 0) > 30

  return (
    <section
      ref={setRoot}
      data-component="Timeline"
      className={`tl${still ? ' tl-still' : ''}${scrolls ? ' is-scrolling' : ''}${tip ? ' has-tip' : ''}`}
      aria-label="Swarm timeline"
    >
      <div className="tl-frame" onPointerLeave={() => setTip(null)}>
        <div className="tl-axis">
          <div className="tl-gutter" aria-hidden="true" />
          <div className="tl-plot" ref={plotRef}>
            <svg width={width} height={axisH} aria-hidden="true" className="tl-axis-svg">
              {axis.map((t) => {
                const x = xOf(shown.window, width, t.at)
                // Not under the label of now.
                if (x > shown.nowX - 3.5 * rem && x < shown.nowX + 0.5 * rem) return null
                return (
                  <g key={t.at} className={t.major ? 'tl-tick is-major' : 'tl-tick'}>
                    <line x1={x} x2={x} y1={axisH - 5} y2={axisH} />
                    <text x={x} y={axisH - 9} textAnchor={x < 24 ? 'start' : 'middle'}>{t.label}</text>
                  </g>
                )
              })}
              <text className="tl-now-label" data-live="now-label" x={shown.nowX - 4} y={axisH - 9} textAnchor="end">{nowLabel}</text>
            </svg>
          </div>
        </div>

        <div className="tl-body">
          <svg className="tl-grid" width={width} height="100%" aria-hidden="true">
            <defs>
              {/* One hatch angle per agent, for forced-colors mode, where fills are not the agent's own. */}
              {[['claude', 45], ['opencode', 135], ['cursor', 0], ['copilot', 90], ['other', 20]].map(([name, angle]) => (
                <pattern key={name} id={`tl-hatch-${name}`} width="6" height="6" patternUnits="userSpaceOnUse" patternTransform={`rotate(${angle})`}>
                  <rect width="6" height="6" className="tl-hatch-ground" />
                  <line x1="0" y1="0" x2="0" y2="6" className="tl-hatch-line" strokeWidth="2" />
                </pattern>
              ))}
              <radialGradient id="tl-glow">
                <stop offset="0" className="tl-glow-stop" stopOpacity="0.9" />
                <stop offset="0.45" className="tl-glow-stop" stopOpacity="0.45" />
                <stop offset="1" className="tl-glow-stop" stopOpacity="0" />
              </radialGradient>
              <linearGradient id="tl-fade" x1="0" x2="1" y1="0" y2="0">
                <stop offset="0" className="tl-fade-stop" stopOpacity="0" />
                <stop offset="1" className="tl-fade-stop" stopOpacity="0.7" />
              </linearGradient>
            </defs>
            {axis.map((t) => {
              const x = xOf(shown.window, width, t.at)
              return <line key={t.at} className={t.major ? 'tl-gridline is-major' : 'tl-gridline'} x1={x} x2={x} y1={0} y2="100%" />
            })}
          </svg>
          <svg className="tl-now" width={width} height="100%" aria-hidden="true">
            <line className="tl-now-line" data-live="now" x1={shown.nowX} x2={shown.nowX} y1={0} y2="100%" />
          </svg>

          {rows === 0 ? (
            <p className="tl-empty">{empty ?? 'Nothing ran in this range.'}</p>
          ) : (
            shown.groups.map((grp) => (
              <Group
                key={grp.repo}
                group={grp}
                collapsed={collapsed.has(grp.repo)}
                onToggle={toggle}
                width={width}
                g={g}
                now={now}
                agent={agent}
                onTip={onTip}
              />
            ))
          )}
        </div>

        <div className="tl-spend">
          <div className="tl-gutter tl-spend-name">
            <span>Spend</span>
          </div>
          <div className="tl-plot">
            <Spend band={shown.spend} layout={shown} width={width} onTip={onTip} />
          </div>
        </div>
      </div>
      <TimelineTooltip at={tip} now={now} boundary={root} />
    </section>
  )
}
