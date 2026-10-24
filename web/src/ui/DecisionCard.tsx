import type { MouseEvent, ReactNode } from 'react'
import { Button } from './Button'
import { cx } from './cx'
import { OFFLINE_SEND } from './ContextComposer'
import { DecisionOptions, type DecisionOptionsProps } from './DecisionOptions'
import { useOffline } from './offline'
import './Decision.css'

export interface DecisionCardProps {
  /** Who asked: the decision chip, the lane (a link), the agent and run, and how long it has waited. */
  meta: ReactNode
  /** The one-line question. */
  question: ReactNode
  options: DecisionOptionsProps['options']
  recommended?: string
  picked?: string
  onPick?: (option: string) => void
  /** The card is the one open in the main column: a lichen outline. */
  selected?: boolean
  /** The decision waits on the person and has not been opened. */
  unseen?: boolean
  /** A click on the card, away from a link or a button, opens the decision. */
  onOpen?: () => void
  /** Send the picked option, or at 390px the own answer when written. Absent: the card only offers to open the decision. */
  onSend?: () => void
  sending?: boolean
  /** The words of the send button: the picked option, cut short. */
  sendLabel?: string
  /** The 390px card: options at least 44px tall, an own-answer field, a full-width send button. */
  mobile?: boolean
  own?: string
  onOwn?: (text: string) => void
  /** After an answer: what was sent and what happens next, in place of the options. */
  done?: ReactNode
  error?: string
  /** Below the options: the plan's file chip and "Open the full decision". */
  more?: ReactNode
  /** Names the card for a screen reader. */
  label: string
}

/**
 * A decision in the thread (`.sh-card`): a lantern bar on the left, the question, the options as
 * numbered chips and, once one is picked, a quick send. Only the person's own click sends.
 */
export function DecisionCard({
  meta, question, options, recommended, picked, onPick, selected, unseen, onOpen, onSend, sending, sendLabel, mobile, own, onOwn, done, error, more, label,
}: DecisionCardProps) {
  const open = (e: MouseEvent) => {
    if (!onOpen || (e.target as HTMLElement).closest('a, button, input, textarea, [role="radio"]')) return
    onOpen()
  }
  const { offline } = useOffline()
  const ready = Boolean(picked) || (mobile && Boolean(own?.trim()))
  const send = (
    <Button variant="attention" disabled={!ready || sending || offline} title={offline ? OFFLINE_SEND : undefined} onClick={onSend} className={cx(mobile && 'sh-tap sh-card-send')}>
      {sending ? 'Sending…' : mobile ? `Send answer: ${sendLabel ?? picked ?? ''}` : `Send: ${sendLabel ?? picked ?? ''}`}
    </Button>
  )
  return (
    <section
      className={cx('sh-card', selected && 'is-selected')}
      data-component="DecisionCard"
      aria-label={label}
      onClick={open}
    >
      <div className="sh-card-meta">
        {unseen && <span className="sh-unseen" role="img" aria-label="unseen" />}
        {meta}
      </div>
      <h3>{question}</h3>
      {done ?? (
        <>
          {options.length > 0 && (
            <DecisionOptions options={options} variant="chips" picked={picked} recommended={recommended} onPick={onPick} label={`Options for ${label}`} tall={mobile} />
          )}
          {mobile && onOwn && (
            <div className="sh-card-own">
              <input className="sh-input sh-tap" value={own ?? ''} placeholder="Or write your own answer" aria-label={`Your own answer to ${label}`} onChange={(e) => onOwn(e.target.value)} />
              {onSend && send}
            </div>
          )}
          {!mobile && onSend && picked && (
            <div className="sh-card-quick">
              {send}
              <span className="sh-card-hint">or open the decision to edit</span>
            </div>
          )}
          {error && (
            <p className="sh-card-error" role="alert">
              Not sent: {error}
            </p>
          )}
        </>
      )}
      {more && <div className="sh-card-more">{more}</div>}
    </section>
  )
}
