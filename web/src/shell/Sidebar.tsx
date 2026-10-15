import { useState, type ReactNode } from 'react'
import { href, type Route } from '../router'
import type { Status, Waiting } from '../api/types'
import { KeepGoing } from '../components/KeepGoing'
import { Badge, type BadgeTone } from '../ui'
import { COUNT_LABEL, NAV, navCounts, type NavKey } from './nav'

/** Only Inbox is a call to act; Next up is a plain count and Board is broken work (the mockup's three tones). */
const COUNT_TONE: Partial<Record<NavKey, BadgeTone>> = { next: 'neutral', board: 'bad' }

/** The workspace the daemon serves is the brand: its name, or a count when there are several. */
function workspaceName(workspaces: Status['workspaces']): string {
  const [first] = workspaces
  if (!first) return 'uBixShepherd'
  return workspaces.length === 1 ? first.name : `${workspaces.length} workspaces`
}

function Wordmark({ workspaces }: { workspaces: Status['workspaces'] }) {
  return (
    <a
      className="wordmark"
      href={href.board()}
      aria-label={workspaces.length ? `uBixShepherd, ${workspaceName(workspaces)}, go to the board` : 'uBixShepherd, go to the board'}
      title={workspaces.map((w) => w.name).join(', ') || undefined}
    >
      <svg className="wordmark-mark" viewBox="0 0 32 32" aria-hidden="true" focusable="false">
        <rect width="32" height="32" fill="#0f62fe" />
        <rect x="6" y="6" width="9" height="9" fill="#ffffff" />
        <rect x="17" y="6" width="9" height="9" fill="#ffffff" opacity="0.55" />
        <rect x="6" y="17" width="9" height="9" fill="#ffffff" opacity="0.55" />
        <rect x="17" y="17" width="9" height="9" fill="#ffffff" />
      </svg>
      <span className="wordmark-text">{workspaceName(workspaces)}</span>
    </a>
  )
}

/** Detect platform for the search shortcut hint: Cmd on Apple platforms, Ctrl elsewhere. */
function isMacOS(): boolean {
  return /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent)
}

/**
 * The left sidebar: the workspace as the brand, the views in three groups with their counts, then the
 * footer: keep-going, daemon status, update notice. Counts come from the attention
 * list (decisions, next); the board count shows broken lanes, as red text, not a badge.
 */
export function Sidebar({
  route,
  waiting,
  workspaces,
  footer,
  boardBroken,
  paletteOpen,
}: {
  route: Route
  waiting: Waiting | null
  workspaces: Status['workspaces']
  footer: ReactNode
  boardBroken: number
  paletteOpen?: () => void
}) {
  const [isMac] = useState(isMacOS)
  const counts = navCounts(waiting)
  return (
    <div className="sidebar-inner">
      <div className="sidebar-top">
        <Wordmark workspaces={workspaces} />
        <button
          type="button"
          className="search-trigger"
          onClick={() => paletteOpen?.()}
          aria-label="Search or act"
        >
          <span className="search-icon" aria-hidden="true">⌕</span>
          <span className="search-label">Search or act</span>
          <kbd className="search-key">{isMac ? '⌘K' : 'Ctrl K'}</kbd>
        </button>
      </div>
      <nav className="nav" aria-label="Views">
        {NAV.map((g) => (
          <ul key={g.name} className="nav-group" aria-label={g.name}>
            {g.items.map((it) => {
              const n = (it.key === 'board' ? boardBroken : counts[it.key]) ?? 0
              return (
                <li key={it.key}>
                  <a href={it.to()} aria-current={it.current(route) ? 'page' : undefined} title={it.label}>
                    <span className="nav-mark" aria-hidden="true">
                      {it.mark}
                    </span>
                    <span className="nav-label">{it.label}</span>
                    {n > 0 && (
                      <span className="nav-count">
                        <Badge count={n} tone={COUNT_TONE[it.key] ?? it.countTone ?? 'attention'} label={COUNT_LABEL[it.key]} />
                      </span>
                    )}
                  </a>
                </li>
              )
            })}
          </ul>
        ))}
      </nav>
      <div className="sidebar-foot">
        <KeepGoing />
        {footer}
      </div>
    </div>
  )
}
