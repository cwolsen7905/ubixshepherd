import { useEffect, useId, useRef, type ReactNode } from 'react'
import { cx } from './cx'
import { Kbd } from './Kbd'

export type DialogSize = 'default' | 'wide' | 'palette' | 'sheet'

/**
 * The modal (`.sh-modal`): a native `<dialog>`, so focus is held inside, Esc closes, and focus
 * returns to what had it before; its backdrop is the design's scrim. Rendered only while
 * `open`; `onClose` is called for Esc and a click on the scrim. `alert` makes it a
 * confirmation (role alertdialog). `size` picks the palette frame, the wide keys sheet or the bottom sheet.
 * `actions` sit in the foot, after a left-hand "Esc cancel" hint unless `hint` is false.
 */
export function Dialog({
  open,
  onClose,
  title,
  hideTitle = false,
  size = 'default',
  alert = false,
  hint = true,
  children,
  actions,
}: {
  open: boolean
  onClose: () => void
  title: string
  hideTitle?: boolean
  size?: DialogSize
  alert?: boolean
  hint?: boolean
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
      className={cx('sh-modal', size === 'wide' && 'sh-modal--wide', size === 'palette' && 'sh-modal--palette', size === 'sheet' && 'sh-modal--sheet', alert && 'sh-modal--confirm', 'ui-dialog')}
      data-component="Dialog"
      role={alert ? 'alertdialog' : undefined}
      aria-labelledby={titleId}
      onCancel={(e) => {
        e.preventDefault()
        onClose()
      }}
      onClick={(e) => {
        if (e.target === ref.current) onClose()
      }}
    >
      <h2 id={titleId} className={hideTitle ? 'visually-hidden' : undefined}>
        {title}
      </h2>
      {children}
      {actions && (
        <div className="sh-modal-foot">
          {hint && (
            <>
              <span className="sh-modal-hint">
                <Kbd keys="Esc" />
                cancel
              </span>
              <span className="sh-modal-gap" />
            </>
          )}
          {actions}
        </div>
      )}
    </dialog>
  )
}
