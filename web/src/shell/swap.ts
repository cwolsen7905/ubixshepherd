import { go, href, withOpen, type Open, type Route } from '../router'

/**
 * The layout rule (WEB-012): a side panel is for answering, the main body for reading. A decision
 * is read, so opening one in the Conversation puts it in the main column and moves the thread to
 * the side, filtered to its topic; Esc swaps back. What is answered in a panel (a merge request, a
 * next-up item, a live run) stays a side item with "Open as page".
 */
export const SWAP_KINDS: readonly string[] = ['decision']

/** Whether the route is the Conversation with a main-body item open, so the layouts are swapped. */
export function isSwapped(route: Route): route is Route & { open: Open } {
  return route.page === 'chat' && route.open !== undefined && !route.open.full && SWAP_KINDS.includes(route.open.kind)
}

/** The page an item has of its own, for "Open as page". */
export function pageFor(open: Open): string | undefined {
  if (open.kind === 'lane' || open.kind === 'mr') return href.lane(open.id)
  if (open.kind === 'run') return href.log(open.id)
  if (open.kind === 'decision') return withOpen(href.chat(), { kind: 'decision', id: open.id })
  if (open.kind === 'artifact') return `#/artifacts?view=${open.id}`
  return undefined
}

/** What a release of the divider past 60% does, and what the wash says: swap back, or open the item as a page. */
export function swapAction(route: Route): { label: string; run: () => void } | undefined {
  const open = route.open
  if (!open) return undefined
  if (isSwapped(route)) return { label: 'Release to swap back to the thread', run: () => go(withOpen(window.location.hash, undefined)) }
  const page = pageFor(open)
  return page ? { label: 'Release to open as page', run: () => go(page) } : undefined
}
