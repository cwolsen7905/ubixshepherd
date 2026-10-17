import type { ReactNode } from 'react'

/**
 * The one head row of a page: the title, its chips beside it and its hints or buttons at the right.
 * A page renders it instead of its own `h1`; the shell shows no second title above it from 760px up.
 */
export function PageHead({
  title,
  chips,
  hints,
  sub,
  className,
}: {
  title: ReactNode
  /** Counts and filters that sit beside the title. */
  chips?: ReactNode
  /** Key hints or buttons at the right edge. */
  hints?: ReactNode
  /** An optional line under the row. */
  sub?: ReactNode
  className?: string
}) {
  return (
    <header className={`page-head${className ? ` ${className}` : ''}`}>
      <h1>{title}</h1>
      {chips && <div className="page-chips">{chips}</div>}
      {hints && <div className="page-actions">{hints}</div>}
      {sub && <p className="page-sub">{sub}</p>}
    </header>
  )
}
