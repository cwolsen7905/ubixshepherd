import type { ReactNode } from 'react'
import { href, type Route } from '../router'
import type { Status, Waiting } from '../api/types'
import { KeepGoing } from '../components/KeepGoing'
import { SpendMeter } from '../components/BudgetMeter'
import { Badge } from '../ui'
import { COUNT_LABEL, NAV, navCounts } from './nav'

function Wordmark() {
  return (
    <a className="wordmark" href={href.board()} aria-label="uBixShepherd, go to the board">
      <svg className="wordmark-mark" viewBox="0 0 32 32" aria-hidden="true" focusable="false">
        <rect width="32" height="32" fill="#0f62fe" />
        <rect x="6" y="6" width="9" height="9" fill="#ffffff" />
        <rect x="17" y="6" width="9" height="9" fill="#ffffff" opacity="0.55" />
        <rect x="6" y="17" width="9" height="9" fill="#ffffff" opacity="0.55" />
        <rect x="17" y="17" width="9" height="9" fill="#ffffff" />
      </svg>
      <span className="wordmark-text">
        uBix<b>Shepherd</b>
      </span>
    </a>
  )
}

/** The workspace switcher: a clickable button that lists available workspaces. */
export function WorkspaceSwitcher({
  workspaces,
  onSelect,
}: {
  workspaces: Status['workspaces']
  onSelect?: (id: number) => void
}) {
  const [first] = workspaces
  if (!first) return null
  if (workspaces.length === 1) {
    return (
      <p className="workspace" title={first.name}>
        Workspace: <span className="workspace-name">{first.name}</span>
      </p>
    )
  }
  return (
    <div className="workspace">
      <button
        type="button"
        className="workspace-btn"
        aria-expanded={false}
        aria-haspopup="listbox"
        onClick={() => onSelect?.(first.id)}
        title={workspaces.map((w) => w.name).join(', ')}
      >
        Workspace: <span className="workspace-name">{workspaces.length} workspaces</span>
      </button>
    </div>
  )
}

/**
 * The left sidebar: name, workspace, the views in three groups with their counts, then the
 * footer: spend, keep-going, daemon status, update notice. Counts come from the attention
 * list (decisions, next); the board count shows broken lanes.
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
  const counts = navCounts(waiting)
  return (
    <div className="sidebar-inner">
      <div className="sidebar-top">
        <Wordmark />
        <WorkspaceSwitcher workspaces={workspaces} />
        <button
          type="button"
          className="search-trigger"
          onClick={() => paletteOpen?.()}
          aria-label="Search or act"
        >
          <span className="search-icon" aria-hidden="true">⌕</span>
          <span className="search-label">Search or act</span>
          <kbd className="search-key">⌘K</kbd>
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
                        <Badge count={n} tone={it.countTone ?? 'attention'} label={COUNT_LABEL[it.key] ?? ''} />
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
        <SpendMeter spend={{ spent_usd: 0, cap_usd: 0 }} />
        <KeepGoing />
        {footer}
      </div>
    </div>
  )
}
