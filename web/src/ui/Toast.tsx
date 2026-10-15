import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import './ui.css'

export type ToastTone = 'neutral' | 'ok' | 'bad'
type Item = { id: number; text: string; tone: ToastTone }

const Ctx = createContext<(text: string, tone?: ToastTone) => void>(() => {})

/** How long a toast stays before it clears itself. */
export const TOAST_MS = 5000

/**
 * Hosts the toasts and their live region: `useToast()` returns the function that adds one. The
 * region is polite, so a sent answer or an error is announced without taking focus.
 */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<Item[]>([])
  const next = useRef(1)
  const timers = useRef(new Set<number>())

  const push = useCallback((text: string, tone: ToastTone = 'neutral') => {
    const id = next.current++
    setItems((l) => [...l, { id, text, tone }])
    const t = window.setTimeout(() => {
      timers.current.delete(t)
      setItems((l) => l.filter((i) => i.id !== id))
    }, TOAST_MS)
    timers.current.add(t)
  }, [])

  useEffect(() => {
    const all = timers.current
    return () => all.forEach((t) => window.clearTimeout(t))
  }, [])

  return (
    <Ctx.Provider value={push}>
      {children}
      <div className="ui-toasts" role="status" aria-live="polite">
        {items.map((i) => (
          <div key={i.id} className={`ui-toast${i.tone === 'neutral' ? '' : ` ui-toast-${i.tone}`}`}>
            {i.text}
          </div>
        ))}
      </div>
    </Ctx.Provider>
  )
}

export function useToast() {
  return useContext(Ctx)
}
