import { useId, type ReactNode } from 'react'
import { cx } from './cx'
import { Kbd } from './Kbd'
import './Decision.css'

export type DecisionOptionsVariant = 'chips' | 'rows'

export interface DecisionOptionsProps {
  /** The choices, in order. The first nine are numbered, and the number keys pick them. */
  options: readonly string[]
  /** `chips`: numbered buttons that wrap, in a card. `rows`: full-width rows, in a decision's page. */
  variant?: DecisionOptionsVariant
  /** The option picked now. */
  picked?: string
  /** The option the agent recommends: marked with a lantern border and the word. */
  recommended?: string
  onPick?: (option: string) => void
  /** Names the group for a screen reader. */
  label: string
  /** Rows as tall as a thumb, for the 390px page. */
  tall?: boolean
  /** How an option shows: markdown, for instance. */
  render?: (option: string) => ReactNode
}

/**
 * The choices of a decision (`.sh-option`). Picking one only selects it: sending is the person's
 * own click on the send button, never this part. Each is a radio in a group, so a screen reader
 * hears "1 of 3".
 */
export function DecisionOptions({ options, variant = 'chips', picked, recommended, onPick, label, tall, render = (o) => o }: DecisionOptionsProps) {
  const uid = useId()
  const rows = variant === 'rows'
  const rec = recommended?.trim()
  return (
    <div
      className={cx(rows ? 'sh-option-list' : 'sh-options')}
      data-component="DecisionOptions"
      data-variant={variant}
      role="radiogroup"
      aria-label={label}
    >
      {options.map((o, i) => {
        const isRec = !!rec && o.trim() === rec
        const on = picked === o
        return (
          <button
            key={`${uid}${i}`}
            type="button"
            role="radio"
            aria-checked={on}
            className={cx('sh-option', rows && 'sh-option--row', isRec && 'is-recommended', on && 'is-picked', tall && 'sh-tap')}
            onClick={() => onPick?.(o)}
          >
            <span className="sh-option-main">
              {i < 9 && <Kbd keys={String(i + 1)} />}
              <span className="sh-option-text">{render(o)}</span>
            </span>
            {isRec && rows && <span className="sh-option-rec">recommended</span>}
            {isRec && !rows && <span className="visually-hidden"> (recommended)</span>}
          </button>
        )
      })}
    </div>
  )
}
