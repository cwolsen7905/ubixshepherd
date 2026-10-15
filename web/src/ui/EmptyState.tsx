import type { ReactNode } from 'react'
import './ui.css'

/** What a list shows when it has nothing: say what is absent and, if there is one, what to do. */
export function EmptyState({ title, children, action }: { title: string; children?: ReactNode; action?: ReactNode }) {
  return (
    <div className="ui-empty">
      <p className="ui-empty-title">{title}</p>
      {children && <p className="ui-empty-body">{children}</p>}
      {action}
    </div>
  )
}
