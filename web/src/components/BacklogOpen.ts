import { useSyncExternalStore, type KeyboardEvent, type MouseEvent } from 'react'
import { go, parse, withOpen, type Open } from '../router'

/** Is `o` the item the inspector holds now? Rows mark themselves with it. */
export function isOpenNow(o: Open | undefined): boolean {
  const now = parse(window.location.hash).open
  return !!o && !!now && now.kind === o.kind && now.id === o.id
}

/** Show `o` in the inspector; a row already open fires no hashchange, so its heading takes focus. */
export function openInInspector(o: Open) {
  if (isOpenNow(o)) document.getElementById('inspector-title')?.focus()
  else go(withOpen(window.location.hash, o))
}

/** The link target of a row that opens `o`: the current page with `open=` set. */
export const openHref = (o: Open) => withOpen(window.location.hash, o)

/** For an `<a>` row: a plain click opens the inspector; a modified click keeps the link's own ways. */
export function openClick(o: Open) {
  return (e: MouseEvent) => {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
    e.preventDefault()
    openInInspector(o)
  }
}

/** Did the event come from a link, button or field inside the row? Those keep their own click and keys. */
export const isInner = (e: { target: EventTarget }) => e.target instanceof HTMLElement && !!e.target.closest('a, button, input, select')

/**
 * For a table row that is not itself a link: focusable, opens on click and on Enter, and says
 * when it is the open one. Links and buttons inside it keep their own click.
 */
export function openRow(o: Open | undefined) {
  if (!o) return {}
  return {
    tabIndex: 0,
    className: 'row-open',
    'aria-current': isOpenNow(o) ? ('true' as const) : undefined,
    onClick: (e: MouseEvent) => {
      if (!isInner(e)) openInInspector(o)
    },
    onKeyDown: (e: KeyboardEvent) => {
      if (e.key === 'Enter' && !isInner(e)) {
        e.preventDefault()
        openInInspector(o)
      }
    },
  }
}

/** What a board or rail item opens: a decision's or request's pane, else its lane. */
export function itemOpen(it: { decision?: { id: number }; request?: { id: number }; laneId?: number }): Open | null {
  if (it.decision) return { kind: 'decision', id: it.decision.id }
  if (it.request) return { kind: 'request', id: it.request.id }
  if (it.laneId !== undefined) return { kind: 'lane', id: it.laneId }
  return null
}

/** The props of an `<a>` that opens `o` in the inspector, marked when it is the open one. */
export function openLink(o: Open) {
  return { href: openHref(o), 'aria-current': isOpenNow(o) ? ('true' as const) : undefined, onClick: openClick(o) }
}

const onHash = (fn: () => void) => {
  window.addEventListener('hashchange', fn)
  return () => window.removeEventListener('hashchange', fn)
}

/** Re-render the caller when the hash changes: a memoized row calls it so its open marker follows. */
export function useHashOpen(): string {
  return useSyncExternalStore(onHash, () => window.location.hash)
}
