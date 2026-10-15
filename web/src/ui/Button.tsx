import type { ButtonHTMLAttributes } from 'react'
import './ui.css'

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger' | 'attention'

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant
  size?: 'normal' | 'small'
}

/** The one button. `attention` is for the action that clears something waiting on the person. One primary button per view. */
export function Button({ variant = 'secondary', size = 'normal', className, type = 'button', ...rest }: ButtonProps) {
  const cls = ['ui-btn', variant !== 'secondary' && `ui-btn-${variant}`, size === 'small' && 'ui-btn-sm', className]
  return <button type={type} className={cls.filter(Boolean).join(' ')} {...rest} />
}
