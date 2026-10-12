import { useEffect, useState } from 'react'
import { daemonInfo } from '../api/client'
import type { NextList, SpendToday, Status, StatusDetail } from '../api/types'
import { clockTime, since, usd } from '../format'
import {
  BACKLOG_MARKS, BACKLOG_NAMES, RAIL_ROWS, backlogCounts, backlogText, readRailOpen, writeRailOpen, type BacklogItem,
} from '../model/backlog'
import { budgetUse } from '../model/projects'
import {
  NEXT_MARKER, readRailView, startText, canStart, planLabel, railItems, waitingCount, writeRailView, type RailView,
} from '../model/next'
import { loadNext, useNextList, type LoadNext } from '../state/next'
import { href } from '../router'
import { Glyph } from './Glyph'
import { MrBadge } from './MrBadge'
import '../styles/next.css'

/** Where a row leads: a decision to its answer, a lane to its page. */
function target(it: BacklogItem): string | null {
  if (it.decision) return href.decisions(it.decision.id)
  if (it.request) return href.decisions()
  if (it.laneId !== undefined) return href.lane(it.laneId)
  return null
}

/** Whether the rail starts open: the stored choice, else open unless the screen is narrow. */
function startsOpen(): boolean {
  const stored = readRailOpen(window.localStorage)
  if (stored !== null) return stored
  return !window.matchMedia?.('(max-width: 760px)').matches
}

const QUOTA_POLL = 30_000

/** The daemon's detailed status, re-read slowly: quota holds and the forge poll's health. */
function useDetail(load: () => Promise<Status>) {
  const [detail, setDetail] = useState<StatusDetail | undefined>()
  useEffect(() => {
    let live = true
    const read = () =>
      load().then(
        (s) => live && setDetail(s.detail),
        () => {},
      )
    void read()
    const id = window.setInterval(() => void read(), QUOTA_POLL)
    return () => {
      live = false
      window.clearInterval(id)
    }
  }, [load])
  return detail
}

/**
 * The backlog beside the thread: counts as its header, then the items most urgent first.
 * A row's name leads to its page; "in thread" jumps to where the work last spoke.
 */
export function Backlog({
  items, now, spend, onJump, onStart, loadStatus = daemonInfo.status, load = loadNext,
}: {
  items: BacklogItem[]
  now: number
  spend: SpendToday | null
  onJump: (anchor: string) => void
  /** Puts a next-up item's request in the chat draft; the Start button is absent without it. */
  onStart?: (text: string) => void
  loadStatus?: () => Promise<Status>
  load?: LoadNext
}) {
  const [open, setOpen] = useState(startsOpen)
  const [all, setAll] = useState(false)
  const [view, setView] = useState<RailView>(() => readRailView(window.localStorage))
  const { list: nextList } = useNextList(false, load)
  const detail = useDetail(loadStatus)
  const holds = detail?.quota_holds ?? []
  const forge = detail?.forge
  const text = backlogText(backlogCounts(items))
  const shown = all ? items : items.slice(0, RAIL_ROWS)
  const use = spend && spend.budget > 0 ? budgetUse({ amount: spend.budget, spent: spend.usd, cap: '', period: 'day', since: spend.day }) : null

  return (
    <aside className="backlog" aria-label="Backlog">
      <h2 className="backlog-head">
        <button
          type="button"
          className="backlog-toggle"
          aria-expanded={open}
          aria-controls="backlog-body"
          onClick={() => {
            writeRailOpen(window.localStorage, !open)
            setOpen(!open)
          }}
        >
          <span className="backlog-title">Backlog</span>{' '}
          <span className="backlog-counts">{text || 'nothing waiting'}</span>
        </button>
      </h2>
      <div className="backlog-views" role="group" aria-label="Rail view">
        {(['now', 'next'] as const).map((v) => (
          <button
            key={v}
            type="button"
            aria-pressed={view === v}
            onClick={() => {
              writeRailView(window.localStorage, v)
              setView(v)
              if (!open) {
                writeRailOpen(window.localStorage, true)
                setOpen(true)
              }
            }}
          >
            {v === 'now' ? 'Now' : `Next up${nextList && waitingCount(nextList) > 0 ? ` ${waitingCount(nextList)}` : ''}`}
          </button>
        ))}
      </div>
      <div id="backlog-body" className="backlog-body" hidden={!open}>
        {view === 'next' ? (
          <NextView list={nextList} now={now} onStart={onStart} />
        ) : (
          <>
        {spend && (
          <p className="backlog-spend">
            {use ? (
              <span
                className={`meter tone-${use.tone}`}
                role="meter"
                aria-label="Spent today"
                aria-valuemin={0}
                aria-valuemax={spend.budget}
                aria-valuenow={spend.usd}
                aria-valuetext={`${usd(spend.usd) || '$0.00'} of ${usd(spend.budget)}`}
              >
                <span className="meter-fill" style={{ width: `${Math.min(100, use.percent)}%` }} />
              </span>
            ) : null}
            <span>
              {usd(spend.usd) || '$0.00'}
              {use ? ` of ${usd(spend.budget)}` : ''} today
              {use && use.tone !== 'ok' && <span className={`tone-${use.tone}`}>{use.fraction >= 1 ? ' · over budget' : ' · near budget'}</span>}
            </span>
          </p>
        )}
        {forge && (
          <p className={`backlog-forge ${forge.error ? 'tone-bad' : 'faint'}`} role="status">
            <Glyph glyph={forge.error ? '✗' : '●'} tone={forge.error ? 'bad' : 'ok'} label={forge.error ? 'forge poll failing' : 'forge polled'} />{' '}
            {forge.error
              ? `Forge poll failing: ${forge.error}; MR badges may be stale`
              : forge.last_poll
                ? `Forge polled ${since(forge.last_poll, now) || 'just now'} ago`
                : 'Forge not polled yet'}
          </p>
        )}
        {holds.map((h) => (
          <p key={h.agent} className="backlog-quota tone-warn" role="status">
            <Glyph glyph="⏸" tone="warn" label="quota hold" /> {h.agent} is out of quota until {clockTime(h.until, now)}
          </p>
        ))}
        {items.length === 0 ? (
          <p className="faint backlog-empty">Nothing is waiting on you and no lane is idle.</p>
        ) : (
          <ul className="backlog-rows">
            {shown.map((it) => {
              const mark = BACKLOG_MARKS[it.group]
              const to = target(it)
              const label = <span className="backlog-name">{it.name}</span>
              return (
                <li key={it.key} className={`backlog-row group-${it.group}`}>
                  <Glyph glyph={it.glyph ?? mark.glyph} tone={mark.tone} label={BACKLOG_NAMES[it.group]} />
                  <span className="backlog-main">
                    {to ? <a href={to}>{label}</a> : label}
                    <span className="backlog-detail">{it.detail}</span>
                  </span>
                  <span className="backlog-side">
                    <time dateTime={it.at} title={it.at} className="faint">{since(it.at, now) || 'now'}</time>
                    {it.lane && <MrBadge lane={it.lane} />}
                    {it.anchor && (
                      <button type="button" className="link-button backlog-jump" onClick={() => onJump(it.anchor ?? '')}>
                        in thread
                      </button>
                    )}
                  </span>
                </li>
              )
            })}
          </ul>
        )}
        {items.length > RAIL_ROWS && (
          <p className="backlog-more">
            <button type="button" className="link-button" onClick={() => setAll(!all)}>
              {all ? 'Show fewer' : `+${items.length - RAIL_ROWS} more`}
            </button>{' '}
            <a href={href.board()}>Board</a>
          </p>
        )}
          </>
        )}
      </div>
    </aside>
  )
}

/** The top of the next-up list for the rail: the approved items still to start, and a link to the page. */
function NextView({ list, now, onStart }: { list: NextList | null; now: number; onStart?: (text: string) => void }) {
  if (list === null) return <p className="faint backlog-empty">Reading the next-up list…</p>
  const rows = railItems(list)
  const waiting = waitingCount(list)
  return (
    <>
      {rows.length === 0 ? (
        <p className="faint backlog-empty">No approved item is waiting to start.</p>
      ) : (
        <ul className="backlog-rows">
          {rows.map((it) => {
            const isNext = list.next_up === it.id
            return (
              <li key={it.id} className={`backlog-row${isNext ? ' is-next' : ''}`} aria-current={isNext ? 'true' : undefined}>
                <Glyph
                  glyph={isNext ? NEXT_MARKER.glyph : '◇'}
                  tone="accent"
                  label={isNext ? NEXT_MARKER.label : 'approved'}
                />
                <span className="backlog-main">
                  <a href={href.next()} className="backlog-name">{it.title}</a>
                  <span className="backlog-detail">
                    {[it.repo, it.plan ? planLabel(it.plan) : ''].filter(Boolean).join(' · ') || 'no repo or plan'}
                  </span>
                </span>
                <span className="backlog-side">
                  <time dateTime={it.created} title={it.created} className="faint">{since(it.created, now) || 'now'}</time>
                  {onStart && canStart(it) && (
                    <button type="button" className="link-button backlog-start" onClick={() => onStart(startText(it))}>
                      Start
                    </button>
                  )}
                </span>
              </li>
            )
          })}
        </ul>
      )}
      <p className="backlog-more">
        <a href={href.next()}>{waiting > rows.length ? `All ${waiting} approved` : 'Next up page'}</a>
      </p>
    </>
  )
}
