import { useEffect, useId, useRef, type ReactNode } from 'react'
import './ui.css'

/**
 * A native modal `<dialog>`: focus is held inside, Esc closes, and focus returns to what had
 * it before. Rendered only while `open`; `onClose` is called for Esc and the backdrop.
 */
export function Dialog({
  open,
  onClose,
  title,
  children,
  actions,
}: {
  open: boolean
  onClose: () => void
  title: string
  children?: ReactNode
  actions?: ReactNode
}) {
  const ref = useRef<HTMLDialogElement>(null)
  const titleId = useId()
  useEffect(() => {
    if (!open) return
    const d = ref.current
    const was = document.activeElement as HTMLElement | null
    if (d && !d.open) {
      if (d.showModal) d.showModal()
      else d.setAttribute('open', '')
    }
    return () => was?.focus?.()
  }, [open])
  if (!open) return null
  return (
    <dialog
      ref={ref}
      className="ui-dialog"
      aria-labelledby={titleId}
      onCancel={(e) => {
        e.preventDefault()
        onClose()
      }}
      onClick={(e) => {
        if (e.target === ref.current) onClose()
      }}
    >
      <h2 id={titleId} className="ui-dialog-title">
        {title}
      </h2>
      {children}
      {actions && <div className="ui-dialog-actions">{actions}</div>}
    </dialog>
  )
}
