import type { ReactNode } from 'react'
import { cx } from './cx'
import { StatusGlyph, type GlyphTone } from './StatusGlyph'

export type GroupHeaderVariant = 'section' | 'status' | 'project'

export interface GroupHeaderProps {
  /** `section`: a label with an optional hint (Inbox, Next up). `status`: glyph, label and count (Board's status groups). `project`: name, meta and spend. */
  variant?: GroupHeaderVariant
  label: ReactNode
  /** `section`: a faint hint after the label. */
  hint?: ReactNode
  /** `status`: the glyph and its tone. */
  glyph?: string
  glyphTone?: GlyphTone
  /** `status`: how many are in the group. */
  count?: number
  /** Lantern: the group waits on the person. */
  warn?: boolean
  /** `project`: fields after the name. */
  meta?: ReactNode
  /** `project`: today's spend, at the right. */
  spend?: ReactNode
}

/** A group's header: `.sh-group-h` for sections and statuses, `.sh-project-h` for a project. */
export function GroupHeader({ variant = 'section', label, hint, glyph, glyphTone = 'idle', count, warn, meta, spend }: GroupHeaderProps) {
  if (variant === 'project') {
    return (
      <div className="sh-project-h" data-component="GroupHeader" data-variant="project">
        <b>{label}</b>
        {meta && <span className="sh-project-h-meta">{meta}</span>}
        {spend && <span className="sh-project-h-spend">{spend}</span>}
      </div>
    )
  }
  return (
    <div className={cx('sh-group-h', warn && 'sh-group-h--warn', variant === 'status' && 'sh-group-h--status')} data-component="GroupHeader" data-variant={variant}>
      {variant === 'status' && glyph && <StatusGlyph glyph={glyph} tone={glyphTone} />}
      {label}
      {variant === 'status' && count !== undefined && <span className="sh-group-h-n">{count}</span>}
      {variant === 'section' && hint && <span className="sh-group-h-hint">{hint}</span>}
    </div>
  )
}
