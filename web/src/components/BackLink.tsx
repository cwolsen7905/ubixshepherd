import { cameFrom, go, href, parse, withOpen, type Open, type Route } from '../router'
import type { PageChrome } from '../ui/PageHeader'

/** What a route is called in a "back to" link. */
export function routeName(r: Route): string {
  switch (r.page) {
    case 'board': return 'Board'
    case 'runs': return 'Runs'
    case 'inbox': return 'Inbox'
    case 'usage': return 'Usage'
    case 'timeline': return 'Timeline'
    case 'next': return 'Next'
    case 'artifacts': return 'Artifacts'
    case 'history': return 'History'
    case 'files': return 'Files'
    case 'rules': return 'Rules'
    case 'projects': return 'Projects'
    case 'project': return r.name
    case 'unassigned': return 'Unassigned'
    case 'chat': return 'Chat'
    case 'settings': return 'Settings'
    case 'status': return 'Status'
    case 'lane': return `Lane ${r.id}`
    case 'log': return `Run ${r.run}`
    case 'missing': return 'Board'
    case 'kit': return 'Kit'
  }
}

/** Scrolls to y once the page is tall enough to hold it, giving up after about a second (data may still be loading). */
export function restoreScroll(y: number) {
  let frames = 0
  const step = () => {
    const tall = document.documentElement.scrollHeight >= y + window.innerHeight
    if (tall || ++frames > 60) {
      window.scrollTo(0, y)
      return
    }
    window.requestAnimationFrame(step)
  }
  window.requestAnimationFrame(step)
}

/**
 * Takes the person back to the page they came from, with its address (filters, range) and scroll position, labelled for it. Opened
 * directly, it goes to the board, or to the `fallback` address when given.
 */
export function BackLink({ fallback = href.board() }: { fallback?: string } = {}) {
  const came = cameFrom()
  const target = came ? came.hash : fallback
  const label = routeName(came ? parse(came.hash) : parse(fallback))
  return (
    <p className="back">
      <a
        href={target.startsWith('#') ? target : `#${target}`}
        onClick={(e) => {
          if (!came || e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return
          e.preventDefault()
          go(came.hash)
          restoreScroll(came.scroll)
        }}
      >
        ← {label}
      </a>
    </p>
  )
}

/**
 * What a page's header does to leave: Close goes where the back link goes (the page the person
 * came from, at its scroll, else `fallback`), and "Open beside …" shows `open` in that page's
 * side panel. The back link is the shared `BackLink`.
 */
export function pageExit(open: Open | undefined, fallback = href.board()): Omit<PageChrome, 'back'> {
  const came = cameFrom()
  const target = came ? came.hash : fallback
  const name = routeName(parse(target)).toLowerCase()
  return {
    onClose: () => {
      go(target)
      if (came) restoreScroll(came.scroll)
    },
    beside: open ? { label: `Open beside the ${name}`, onClick: () => go(withOpen(target, open)) } : undefined,
  }
}
