import type { ReactNode } from 'react'
import { Button } from './Button'
import { Dialog } from './Dialog'

/**
 * The confirmation modal: what will happen, as a short list, then Cancel and the action.
 * A destructive action is the broken button (`danger`); the person's own commit stays lantern (`warn`).
 */
export function Confirm({
  open,
  title,
  items = [],
  confirmLabel,
  tone = 'warn',
  onConfirm,
  onCancel,
  children,
}: {
  open: boolean
  title: string
  items?: ReactNode[]
  confirmLabel: string
  tone?: 'warn' | 'danger'
  onConfirm: () => void
  onCancel: () => void
  children?: ReactNode
}) {
  return (
    <Dialog
      open={open}
      onClose={onCancel}
      title={title}
      alert
      actions={
        <>
          <Button variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
          <Button variant={tone === 'danger' ? 'broken' : 'attention'} onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      {items.length > 0 && (
        <ul>
          {items.map((x, i) => (
            <li key={i}>{x}</li>
          ))}
        </ul>
      )}
      {children}
    </Dialog>
  )
}
