import { useState, type ReactNode } from 'react'
import { href, type Route } from '../router'
import type { Status, Waiting } from '../api/types'
import { KeepGoing } from '../components/KeepGoing'
import { Count, Kbd, cx, type BadgeTone } from '../ui'
import { COUNT_LABEL, NAV, navCounts, type NavKey } from './nav'

/** Only Inbox is a call to act; Next up is a plain count and Board is broken work (the design's three tones). */
const COUNT_TONE: Partial<Record<NavKey, BadgeTone>> = { next: 'neutral', board: 'bad' }

/** The workspace the daemon serves is the brand: its name, or a count when there are several. */
function workspaceName(workspaces: Status['workspaces']): string {
  const [first] = workspaces
  if (!first) return 'uBixShepherd'
  return workspaces.length === 1 ? first.name : `${workspaces.length} workspaces`
}

/** The workspace mark and name (`.sh-ws`), a link to the board. */
export function Workspace({ workspaces }: { workspaces: Status['workspaces'] }) {
  return (
    <a
      className="sh-ws"
      data-component="WorkspaceMark"
      href={href.board()}
      aria-label={workspaces.length ? `uBixShepherd, ${workspaceName(workspaces)}, go to the board` : 'uBixShepherd, go to the board'}
      title={workspaces.map((w) => w.name).join(', ') || undefined}
    >
      <span className="sh-mark" aria-hidden="true">
        <i />
        <i />
        <i />
        <i />
      </span>
      <span>{workspaceName(workspaces)}</span>
    </a>
  )
}

/** Detect platform for the search shortcut hint: Cmd on Apple platforms, Ctrl elsewhere. */
function isMacOS(): boolean {
  return /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent)
}

/**
 * The left sidebar (`.sh-side`): the workspace as the brand, the search trigger that opens the
 * palette, the views in three groups with their counts and unseen dots, then the footer in the
 * design's order: Keep going, then what the caller puts in `footer` (spend, restarting, the
 * Online line with the keys button, the four status counts, the update notice).
 * Counts come from the attention list; the board count shows broken lanes, in red with a cross.
 */
export function Sidebar({
  route,
  waiting,
  workspaces,
  footer,
  boardBroken,
  paletteOpen,
  unseen,
}: {
  route: Route
  waiting: Waiting | null
  workspaces: Status['workspaces']
  footer: ReactNode
  boardBroken: number
  paletteOpen?: () => void
  /** Views with something new since the person last looked. */
  unseen?: ReadonlySet<NavKey>
}) {
  const [isMac] = useState(isMacOS)
  const counts = navCounts(waiting)
  return (
    <>
      <Workspace workspaces={workspaces} />
      <button type="button" className="sh-find" data-component="SearchTrigger" onClick={() => paletteOpen?.()} aria-label="Search or act">
        <span>Search or act</span>
        <Kbd keys={isMac ? '⌘K' : 'Ctrl K'} />
      </button>
      <nav className="sh-side-nav" aria-label="Views">
        {NAV.map((g, i) => (
          <div key={g.name} role="group" aria-label={g.name} className="sh-nav-group">
            {i > 0 && <div className="sh-nav-sep" aria-hidden="true" />}
            {g.items.map((it) => {
              const n = (it.key === 'board' ? boardBroken : counts[it.key]) ?? 0
              const here = it.current(route)
              return (
                <a
                  key={it.key}
                  className={cx('sh-nav-item', here && 'is-here')}
                  data-component="NavItem"
                  href={it.to()}
                  aria-current={here ? 'page' : undefined}
                  title={it.label}
                >
                  <span className="sh-nav-label">
                    {it.label}
                    {unseen?.has(it.key) && <i className="sh-unseen" title="New since you last looked" />}
                  </span>
                  <Count count={n} tone={COUNT_TONE[it.key] ?? it.countTone ?? 'attention'} label={COUNT_LABEL[it.key]} />
                </a>
              )
            })}
          </div>
        ))}
      </nav>
      <div className="sh-side-foot">
        <KeepGoing />
        {footer}
      </div>
    </>
  )
}
