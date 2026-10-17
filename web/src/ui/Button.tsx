import type { ButtonHTMLAttributes } from 'react'
import { cx } from './cx'
import './ui.css'

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger' | 'attention' | 'broken'

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant
  size?: 'normal' | 'small'
}

/** The class each variant puts on `.sh-btn`. */
const MODIFIER: Record<ButtonVariant, string | false> = {
  secondary: false,
  primary: 'sh-btn--primary',
  attention: 'sh-btn--warn',
  ghost: 'sh-btn--ghost',
  danger: 'sh-btn--danger',
  broken: 'sh-btn--broken',
}

/**
 * The one button. `attention` (lantern) is the person's commit action, the one that clears
 * something waiting on them; `primary` (lichen) is send and go; `broken` is a destructive confirm.
 * The `ui-btn*` names stay on the element as hooks for pages and tests not yet moved; they carry no style.
 */
export function Button({ variant = 'secondary', size = 'normal', className, type = 'button', ...rest }: ButtonProps) {
  const cls = cx('sh-btn', MODIFIER[variant], size === 'small' && 'sh-btn--sm', 'ui-btn', variant !== 'secondary' && `ui-btn-${variant}`, size === 'small' && 'ui-btn-sm', className)
  return <button type={type} className={cls} data-component="Button" {...rest} />
}
