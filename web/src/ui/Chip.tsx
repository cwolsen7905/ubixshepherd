import type { ReactNode } from 'react'
import { cx } from './cx'

/**
 * A filter, file or topic (`.sh-chip`). With `onClick` it is a button (a toggle when `selected`
 * is given, shown by `.is-on`); `onRemove` adds a remove button. `count` is the lantern number
 * of items waiting on the person. Otherwise it is plain text.
 */
export function Chip({
  children,
  kind = 'topic',
  selected,
  count,
  onClick,
  onRemove,
}: {
  children: ReactNode
  kind?: 'filter' | 'file' | 'topic'
  selected?: boolean
  count?: number
  onClick?: () => void
  onRemove?: () => void
}) {
  const cls = cx('sh-chip', kind === 'file' && 'sh-chip--file', selected && 'is-on', 'ui-chip')
  const n = count ? <span className="sh-chip-n">{count}</span> : null
  if (onRemove) {
    return (
      <span className={cls} data-component="Chip">
        {children}
        {n}
        <button type="button" className="sh-chip-x" aria-label="Remove" onClick={onRemove}>
          ×
        </button>
      </span>
    )
  }
  if (onClick) {
    return (
      <button type="button" className={cls} data-component="Chip" aria-pressed={selected} onClick={onClick}>
        {children}
        {n}
      </button>
    )
  }
  return (
    <span className={cls} data-component="Chip">
      {children}
      {n}
    </span>
  )
}
