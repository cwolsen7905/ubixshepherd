import type { KeyboardEvent, ReactNode } from 'react'
import { Button } from './Button'
import { cx } from './cx'
import { useOffline } from './offline'

export type ContextComposerVariant = 'footer' | 'composer'

export interface ContextComposerProps {
  /** `footer`: the side panel's foot (surface, top rule, an "about" line). `composer`: the rounded composer under a list or thread. */
  variant?: ContextComposerVariant
  /** `footer`: what the message is about. */
  about?: ReactNode
  placeholder?: string
  /** A controlled draft. Without `onChange` the part is a static placeholder that opens on click. */
  value?: string
  onChange?: (value: string) => void
  onSend?: (value: string) => void
  sendLabel?: string
  /** `composer`: sits directly under the pending bar and shares its border. */
  joined?: boolean
  busy?: boolean
  disabled?: boolean
  /** Opens the real composer from the static form. */
  onOpen?: () => void
  /** Anything else for the foot, instead of the field. */
  children?: ReactNode
}

/** Why Send is off while the daemon does not answer. */
export const OFFLINE_SEND = 'Offline: Send is off until the daemon answers'

/**
 * The text field at the bottom of a side panel or list. Offline, Send is disabled and the draft
 * stays where it is: the draft is the caller's `value`, which this part never clears.
 * Command-Enter or Ctrl-Enter sends.
 */
export function ContextComposer({
  variant = 'footer',
  about,
  placeholder,
  value,
  onChange,
  onSend,
  sendLabel = 'Send',
  joined,
  busy,
  disabled,
  onOpen,
  children,
}: ContextComposerProps) {
  const { offline } = useOffline()
  const live = onChange !== undefined
  const blank = !(value ?? '').trim()
  const cannot = Boolean(disabled || busy || offline || blank)
  const send = () => {
    if (!cannot) onSend?.(value ?? '')
  }
  const onKey = (e: KeyboardEvent) => {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault()
      send()
    }
  }
  const field = live ? (
    <textarea
      aria-label={placeholder ?? 'Message'}
      placeholder={placeholder}
      value={value ?? ''}
      rows={variant === 'footer' ? 2 : 1}
      onChange={(e) => onChange?.(e.target.value)}
      onKeyDown={onKey}
    />
  ) : (
    <span className={variant === 'footer' ? 'sh-ctx-field' : 'sh-composer-hint'}>{placeholder}</span>
  )
  const sendButton = onSend && (
    <Button variant="primary" size={variant === 'footer' ? 'small' : 'normal'} disabled={cannot} title={offline ? OFFLINE_SEND : undefined} onClick={send}>
      {sendLabel}
    </Button>
  )
  if (variant === 'footer') {
    return (
      <div className="sh-ctx" data-component="ContextComposer" data-variant="footer">
        {about && <div className="sh-ctx-about">{about}</div>}
        {children ?? field}
        {sendButton && <div className="sh-actions">{sendButton}</div>}
      </div>
    )
  }
  return (
    <div
      className={cx('sh-composer', joined && 'is-joined', !live && onOpen && 'is-open')}
      data-component="ContextComposer"
      data-variant="composer"
      onClick={!live ? onOpen : undefined}
    >
      {children ?? field}
      {sendButton}
    </div>
  )
}
