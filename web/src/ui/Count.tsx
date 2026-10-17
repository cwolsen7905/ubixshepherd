import { cx } from './cx'

export type BadgeTone = 'neutral' | 'attention' | 'bad' | 'ok'

const MODIFIER: Record<BadgeTone, string | false> = {
  neutral: false,
  attention: 'sh-count--warn',
  bad: 'sh-count--bad',
  ok: 'sh-count--ok',
}

/**
 * A count (`.sh-count`): default, `attention` for what waits on the person, `bad` for broken
 * work (red text, with a cross so it never rests on colour alone). Past `max` it reads "99+";
 * a zero shows nothing unless `showZero`.
 */
export function Count({
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
    <span className={cx('sh-count', MODIFIER[tone])} data-component="Count" aria-label={label ? `${count} ${label}` : undefined}>
      {text}
      {tone === 'bad' && <span aria-hidden="true"> ✗</span>}
    </span>
  )
}

export { Count as Badge }
