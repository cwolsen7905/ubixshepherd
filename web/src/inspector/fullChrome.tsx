import { routeName } from '../components/BackLink'
import { go, useRoute, withOpen, type Open } from '../router'
import { PageBack, type PageChrome } from '../ui/PageHeader'

/** Closes the inspector by dropping `open=` from the address, so Back opens it again. */
export function closeInspector() {
  go(withOpen(window.location.hash, undefined))
}

/** The same item without `full`: the way back from a full-width view. */
export function leaveFull(open: Open) {
  go(withOpen(window.location.hash, { kind: open.kind, id: open.id, ...(open.tab ? { tab: open.tab } : {}) }))
}

/**
 * What the page header of a full-width inspector view does to leave: the back link and Close
 * return to the list the item was opened from, "Open beside …" steps back to the side panel. Esc
 * does the last of these (the frame answers it), so the key chip sits on that button.
 */
export function useFullChrome(open: Open): PageChrome {
  const name = routeName(useRoute())
  return {
    back: <PageBack onClick={closeInspector}>← {name}</PageBack>,
    beside: { label: `Open beside the ${name.toLowerCase()}`, onClick: () => leaveFull(open) },
    onClose: closeInspector,
    escape: 'beside',
    titleId: 'inspector-title',
  }
}
