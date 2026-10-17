import { cx } from './cx'

/** A budget meter (`.sh-meter`): lantern from 80%, broken at the cap. Only the fill's width is inline, as it is computed. */
export function Meter({ value, max, label, valueText }: { value: number; max: number; label: string; valueText?: string }) {
  const fraction = max > 0 ? value / max : 0
  return (
    <div
      className={cx('sh-meter', fraction >= 0.8 && fraction < 1 && 'is-near', fraction >= 1 && 'is-over')}
      data-component="Meter"
      role="meter"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={max}
      aria-valuenow={value}
      aria-valuetext={valueText}
    >
      <b style={{ width: `${Math.min(100, fraction * 100)}%` }} />
    </div>
  )
}
