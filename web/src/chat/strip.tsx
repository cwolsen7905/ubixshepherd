import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import type { WaitingItem } from '../api/types'
import { useKeys } from '../hooks'
import { neighbour, openKey, openTarget, requestStep, waitingKey } from '../model/waitingStep'
import { go, parse, withOpen } from '../router'
import { cutAtWord, fitCount } from '../components/TopicBar'
import { Kbd } from '../ui'
import { useSnapshot } from '../state/context'
import { isUnseen, useSeen } from '../state/seen'

const LABEL: Partial<Record<WaitingItem['kind'], string>> = {
  decision: 'Decision',
  question: 'Question',
  mr: 'Merge request',
  ship: 'To ship',
  next: 'Next up',
  request: 'Request',
}

/** The longest an item's title gets in the strip; the full text is the tooltip. */
export const ITEM_MAX = 24

/** A title cut at a whole word to at most `max` characters, "…" after a cut. */
export const shortTitle = (title: string, max = ITEM_MAX) => cutAtWord(title, max)

/** Shows one waiting item: in the inspector when it has a pane, else in the thread. */
export function showWaiting(it: WaitingItem) {
  const target = openTarget(it)
  if (target) go(withOpen(window.location.hash, target))
  else requestStep(it)
}

/** Opens the item after the one just answered, or closes the inspector when nothing else waits. */
export function advanceFrom(items: readonly WaitingItem[], answered: string) {
  const next = neighbour(items, answered, 'down', answered)
  if (!next) go(withOpen(window.location.hash, undefined))
  else showWaiting(next)
}

/**
 * Above the composer (`.sh-pending`, joined to it): how many things wait on the person, each as
 * a link, with `]` and `[` to step to the next and previous one. Stepping opens the item and
 * leaves the thread where it is. `keys` off leaves the keys to a page that has its own `]`.
 */
export function PendingStrip({ keys = true }: { keys?: boolean }) {
  const list = useSnapshot().waiting?.items
  const seen = useSeen()
  const items = useMemo(() => list ?? [], [list])
  const at = openKey(items, parse(window.location.hash).open)
  const step = useCallback(
    (dir: 'up' | 'down') => {
      const it = neighbour(items, openKey(items, parse(window.location.hash).open), dir)
      if (it) showWaiting(it)
    },
    [items],
  )
  const box = useRef<HTMLUListElement>(null)
  const [menu, setMenu] = useState(false)
  const [fit, setFit] = useState<number | null>(null)
  // Every item is measured (the ones past the fit stay in the DOM, invisible); as many as fit show.
  useLayoutEffect(() => {
    const el = box.current
    if (!el) return
    const measure = () => {
      const widths = Array.from(el.querySelectorAll('button'), (c) => c.offsetWidth)
      const n = el.clientWidth === 0 ? widths.length : fitCount(widths, el.clientWidth - 2)
      setFit((f) => (f === n ? f : n))
    }
    measure()
    if (typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [items])
  useEffect(() => {
    if (!menu) return
    const off = (e: Event) => {
      if (e instanceof KeyboardEvent ? e.key === 'Escape' : !(e.target as Element).closest('.sh-pending-more')) setMenu(false)
    }
    document.addEventListener('keydown', off)
    document.addEventListener('mousedown', off)
    return () => {
      document.removeEventListener('keydown', off)
      document.removeEventListener('mousedown', off)
    }
  }, [menu])
  useKeys(keys ? { ']': () => step('down'), '[': () => step('up') } : {}, [step, keys])
  if (items.length === 0) return null
  return (
    <nav className="sh-pending" data-component="PendingBar" aria-label="Waiting on you">
      <b>{items.length} waiting on you</b>
      <ul ref={box} className="sh-pending-list">
        {items.map((it, i) => {
          const key = waitingKey(it)
          const over = fit !== null && i >= fit
          return (
            <li key={key}>
              <button
                type="button" className={`sh-pending-item${over ? ' is-over' : ''}`} aria-current={key === at ? 'true' : undefined}
                title={`${LABEL[it.kind] ?? it.kind}: ${it.title}`} tabIndex={over ? -1 : undefined} onClick={() => showWaiting(it)}
              >
                {isUnseen(seen, it) ? <span className="sh-unseen" role="img" aria-label="unseen" /> : null}
                {LABEL[it.kind] ?? it.kind}: {shortTitle(it.title)}
              </button>
            </li>
          )
        })}
      </ul>
      {fit !== null && fit < items.length && (
        <span className="sh-pending-more">
          <button type="button" className="sh-pending-item sh-pending-more-btn" aria-expanded={menu} aria-haspopup="true" title={`${items.length - fit} more waiting`} onClick={() => setMenu(!menu)}>
            +{items.length - fit}
          </button>
          {menu && (
            <div className="sh-tmore-pop sh-tmore-pop--up" role="group" aria-label="More waiting items">
              {items.slice(fit).map((it) => (
                <button key={waitingKey(it)} type="button" className="sh-tmore-item" title={it.title} onClick={() => { setMenu(false); showWaiting(it) }}>
                  <span>{LABEL[it.kind] ?? it.kind}: {shortTitle(it.title, 40)}</span>
                </button>
              ))}
            </div>
          )}
        </span>
      )}
      {keys && (
        <span className="sh-pending-next">
          <Kbd keys="]" />
          next
        </span>
      )}
    </nav>
  )
}
