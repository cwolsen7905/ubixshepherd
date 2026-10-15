import { cameFrom, go, href, parse, type Route } from '../router'

/** What a route is called in a "back to" link. */
export function routeName(r: Route): string {
  switch (r.page) {
    case 'board': return 'Board'
    case 'runs': return 'Runs'
    case 'inbox': return 'Inbox'
    case 'usage': return 'Usage'
    case 'timeline': return 'Timeline'
    case 'next': return 'Next'
    case 'projects': return 'Projects'
    case 'project': return r.name
    case 'unassigned': return 'Unassigned'
    case 'chat': return 'Chat'
    case 'settings': return 'Settings'
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
 * directly, it goes to the board.
 */
export function BackLink() {
  const came = cameFrom()
  const target = came ? came.hash : href.board()
  const label = routeName(came ? parse(came.hash) : { page: 'board' })
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
