import type { ReactNode } from 'react'
import './ui.css'

/**
 * A filter, file or topic. With `onClick` it is a button (a toggle when `selected` is given);
 * `onRemove` adds a remove button. Otherwise it is plain text.
 */
export function Chip({
  children,
  kind = 'topic',
  selected,
  onClick,
  onRemove,
}: {
  children: ReactNode
  kind?: 'filter' | 'file' | 'topic'
  selected?: boolean
  onClick?: () => void
  onRemove?: () => void
}) {
  const cls = `ui-chip${kind === 'file' ? ' ui-chip-file' : ''}`
  if (onRemove) {
    return (
      <span className={cls}>
        {children}
        <button type="button" className="ui-chip-remove" aria-label="Remove" onClick={onRemove}>
          ×
        </button>
      </span>
    )
  }
  if (onClick) {
    return (
      <button type="button" className={cls} aria-pressed={selected} onClick={onClick}>
        {children}
      </button>
    )
  }
  return <span className={cls}>{children}</span>
}
