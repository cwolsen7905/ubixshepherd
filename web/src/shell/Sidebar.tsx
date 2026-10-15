import type { ReactNode } from 'react'
import { href, type Route } from '../router'
import type { Status, Waiting } from '../api/types'
import { KeepGoing } from '../components/KeepGoing'
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

/** The workspace the daemon serves; a name when there is one, a count when there are several. */
export function WorkspaceLabel({ workspaces }: { workspaces: Status['workspaces'] }) {
  const [first] = workspaces
  if (!first) return null
  const text = workspaces.length === 1 ? first.name : `${workspaces.length} workspaces`
  const title = workspaces.map((w) => w.name).join(', ')
  return (
    <p className="workspace" title={title}>
      <span className="faint">Workspace</span> <span className="workspace-name">{text}</span>
    </p>
  )
}

/**
 * The left sidebar: name, workspace, the views in three groups with their counts, then the
 * footer: the Keep going switch and the footer slot. Counts come from the attention list, so they agree with Waiting on you.
 */
export function Sidebar({
  route,
  waiting,
  workspaces,
  footer,
}: {
  route: Route
  waiting: Waiting | null
  workspaces: Status['workspaces']
  footer: ReactNode
}) {
  const counts = navCounts(waiting)
  return (
    <div className="sidebar-inner">
      <div className="sidebar-top">
        <Wordmark />
        <WorkspaceLabel workspaces={workspaces} />
      </div>
      <nav className="nav" aria-label="Views">
        {NAV.map((g) => (
          <ul key={g.name} className="nav-group" aria-label={g.name}>
            {g.items.map((it) => {
              const n = counts[it.key] ?? 0
              return (
                <li key={it.key}>
                  <a href={it.to()} aria-current={it.current(route) ? 'page' : undefined} title={it.label}>
                    <span className="nav-mark" aria-hidden="true">
                      {it.mark}
                    </span>
                    <span className="nav-label">{it.label}</span>
                    <span className="nav-count">
                      <Badge count={n} tone="attention" label={COUNT_LABEL[it.key]} />
                    </span>
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
