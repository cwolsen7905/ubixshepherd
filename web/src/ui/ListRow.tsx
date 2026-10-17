import type { KeyboardEvent, ReactNode } from 'react'
import { cx } from './cx'
import { StatusGlyph, type GlyphTone } from './StatusGlyph'

export type ListRowVariant = 'item' | 'lane' | 'file'

const GRID: Record<ListRowVariant, string> = { item: 'sh-row--inbox', lane: 'sh-row--lane', file: 'sh-row--file' }

export interface ListRowProps {
  /** `item`: glyph, two lines, age (Inbox, Next up). `lane`: adds the merge request column and a bold title. `file`: a mono sub-line and a tag. */
  variant?: ListRowVariant
  glyph: string
  tone?: GlyphTone
  /** The words for the glyph, read out in its place. */
  glyphLabel?: string
  title: ReactNode
  /** Secondary fields, joined with " · " by the caller. */
  sub?: ReactNode
  /** Sits under the sub-line: a blocked tag. */
  badge?: ReactNode
  /** `lane`: the merge request ("!41 ✓"). `file`: a tag. */
  right?: ReactNode
  rightTone?: 'ok' | 'bad' | 'muted'
  age?: string
  /** A 6px lichen dot before the age. */
  unseen?: boolean
  selected?: boolean
  onSelect?: () => void
  /** A link row: it navigates instead of selecting. */
  href?: string
  /** The full title, when the visible one is cut or shortened. */
  titleAttr?: string
}

/**
 * A list row (`.sh-row`), always two lines: the primary text over a sub-line. Only the trailing
 * short columns are fixed, so the primary track never collapses in a narrow centre column.
 * A selected row is lichen-washed; Enter or Space on a focused row selects it.
 */
export function ListRow({
  variant = 'item',
  glyph,
  tone = 'idle',
  glyphLabel,
  title,
  sub,
  badge,
  right,
  rightTone = 'muted',
  age,
  unseen,
  selected,
  onSelect,
  href,
  titleAttr,
}: ListRowProps) {
  const cls = cx('sh-row', GRID[variant], selected && 'is-selected')
  const body = (
    <>
      <StatusGlyph glyph={glyph} tone={tone} label={glyphLabel} />
      <span className="sh-row-main">
        <span className="sh-row-title" title={titleAttr}>{title}</span>
        {sub && <span className="sh-row-sub">{sub}</span>}
        {badge && <span className="sh-row-badge">{badge}</span>}
      </span>
      {variant !== 'item' && <span className={cx('sh-row-right', rightTone !== 'muted' && `is-${rightTone}`)}>{right}</span>}
      <span className="sh-row-age">
        {unseen && <i className="sh-unseen" title="New since you last looked" />}
        {age}
      </span>
    </>
  )
  if (href) {
    return (
      <a className={cls} data-component="ListRow" data-variant={variant} href={href} aria-current={selected ? 'true' : undefined}>
        {body}
      </a>
    )
  }
  const onKey = (e: KeyboardEvent) => {
    if (e.target !== e.currentTarget) return
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      onSelect?.()
    }
  }
  return (
    <div
      className={cls}
      data-component="ListRow"
      data-variant={variant}
      role={onSelect ? 'button' : undefined}
      tabIndex={onSelect ? 0 : undefined}
      aria-pressed={onSelect ? Boolean(selected) : undefined}
      onClick={onSelect}
      onKeyDown={onSelect ? onKey : undefined}
    >
      {body}
    </div>
  )
}
