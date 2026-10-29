import { useCallback, useEffect, useRef, useState } from 'react'
import type { StatusCounts as Counts } from '../state/attention'
import { cx, StatusGlyph } from '../ui'
import { Flyout } from './Flyout'
import { COUNT_KINDS, POP_MAX, SPEC, type CountKind, type PopItem } from './statusItems'

/** How long the popover stays after the pointer leaves, so it can reach the card. */
export const CLOSE_MS = 150

/** The popover's title for a count. */
export function popTitle(kind: CountKind, n: number): string {
  const s = SPEC[kind]
  return n === 0 ? s.none : s.title.replace('{n}', String(n))
}

/** The card's body: a title, up to five one-line items, and "See all n →" past them. Also the 390 bottom sheet's body. */
export function CountList({ kind, n, items, onPick }: { kind: CountKind; n: number; items: PopItem[]; onPick?: () => void }) {
  const s = SPEC[kind]
  const shown = items.slice(0, POP_MAX)
  return (
    <div className="sh-count-pop-card" data-component="CountList">
      <div className="sh-count-pop-h">{popTitle(kind, n)}</div>
      {shown.map((x) => (
        <a key={x.key} className="sh-pop-item" href={x.href} title={x.full} onClick={onPick}>
          <StatusGlyph glyph={x.glyph} tone={x.tone} />
          <span className="sh-pop-text">
            <span className="sh-pop-title">{x.title}</span>
            <span className="sh-pop-where">{x.where}</span>
          </span>
          <span className="sh-pop-age">{x.age}</span>
        </a>
      ))}
      {items.length > POP_MAX && (
        <a className="sh-pop-more" href={s.to()} onClick={onPick}>
          See all {items.length} →
        </a>
      )}
    </div>
  )
}

/**
 * The four status counts: running, waiting on you, broken, needing routing. Each is a glyph and
 * a number and is never hidden; zero is faint. Hover or Tab opens a popover on a layer above the columns, beside the icon
 * (flipped inward at the window's edge; Esc or leaving closes it); the count itself links to Board or Inbox. With `sheet`, a count
 * calls `onSheet` instead, for the bottom sheet at 390.
 */
export function StatusCounts({
  counts,
  lists,
  sheet = false,
  onSheet,
}: {
  counts: Counts
  lists: Record<CountKind, PopItem[]>
  sheet?: boolean
  onSheet?: (kind: CountKind) => void
}) {
  const [open, setOpen] = useState<CountKind | null>(null)
  const timer = useRef<number | undefined>(undefined)
  const [item, setItem] = useState<HTMLElement | null>(null)
  const pop = useRef<HTMLDivElement>(null)
  const show = useCallback((k: CountKind) => {
    window.clearTimeout(timer.current)
    setOpen(k)
  }, [])
  const hide = useCallback(() => {
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setOpen(null), CLOSE_MS)
  }, [])
  useEffect(() => () => window.clearTimeout(timer.current), [])
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(null)
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open])

  return (
    <div className={cx('sh-counts', sheet && 'sh-mobile-counts')} data-component="StatusCounts" role="group" aria-label="Status counts">
      {COUNT_KINDS.map((k) => {
        const s = SPEC[k]
        const n = counts[k]
        return (
          <div
            key={k}
            ref={open === k ? setItem : undefined}
            className="sh-counts-item"
            onMouseEnter={sheet ? undefined : () => show(k)}
            onMouseLeave={sheet ? undefined : hide}
            onFocus={sheet ? undefined : () => show(k)}
            onBlur={
              sheet
                ? undefined
                : (e) => {
                    const to = e.relatedTarget as Node | null
                    if (!e.currentTarget.contains(to) && !pop.current?.contains(to)) hide()
                  }
            }
          >
            <a
              className={cx('sh-count-link', n === 0 && 'is-zero')}
              href={s.to()}
              aria-label={`${n} ${s.noun}`}
              data-count={k}
              onClick={
                sheet
                  ? (e) => {
                      e.preventDefault()
                      onSheet?.(k)
                    }
                  : undefined
              }
              onKeyDown={(e) => {
                // The card is drawn in the body, so Tab would skip it: carry focus in.
                if (e.key !== 'Tab' || e.shiftKey || sheet || open !== k) return
                const first = pop.current?.querySelector<HTMLElement>('a')
                if (!first) return
                e.preventDefault()
                first.focus()
              }}
            >
              <StatusGlyph glyph={s.glyph} tone={n === 0 ? 'idle' : s.tone} />
              {n}
            </a>
            {!sheet && open === k && (
              <Flyout
                ref={pop}
                anchor={item}
                className="sh-count-pop"
                onMouseEnter={() => show(k)}
                onMouseLeave={hide}
                onFocus={() => show(k)}
                onBlur={(e) => {
                  const to = e.relatedTarget as Node | null
                  if (!pop.current?.contains(to) && !item?.contains(to)) hide()
                }}
                onKeyDown={(e) => {
                  if (e.key !== 'Tab' || !e.shiftKey) return
                  if (document.activeElement === pop.current?.querySelector('a')) {
                    e.preventDefault()
                    item?.querySelector<HTMLElement>('a')?.focus()
                  }
                }}
              >
                <CountList kind={k} n={n} items={lists[k]} onPick={() => setOpen(null)} />
              </Flyout>
            )}
          </div>
        )
      })}
    </div>
  )
}
