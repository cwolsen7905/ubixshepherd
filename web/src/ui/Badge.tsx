import './ui.css'

export type BadgeTone = 'neutral' | 'attention' | 'bad' | 'ok'

/** A count. Past `max` it reads "99+"; a zero shows nothing unless `showZero`. */
export function Badge({
  count,
  tone = 'neutral',
  max = 99,
  showZero = false,
  label,
}: {
  count: number
  tone?: BadgeTone
  max?: number
  showZero?: boolean
  /** What is counted, for people using a screen reader: "3 waiting on you". */
  label?: string
}) {
  if (count <= 0 && !showZero) return null
  const text = count > max ? `${max}+` : String(count)
  return (
    <span className={`ui-badge${tone === 'neutral' ? '' : ` ui-badge-${tone}`}`} aria-label={label ? `${count} ${label}` : undefined}>
      {text}
    </span>
  )
}
