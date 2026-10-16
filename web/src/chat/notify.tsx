import { useState } from 'react'
import type { WaitingItem } from '../api/types'
import { announceText, notifiable, seenStore, useSeen } from '../state/seen'

type Perm = NotificationPermission | 'unsupported'
const permission = (): Perm => (typeof Notification === 'undefined' ? 'unsupported' : Notification.permission)

/** True when a notification may be shown now: the person opted in, the browser agrees. */
export const mayNotify = (on: boolean) => on && permission() === 'granted'

/** Shows a browser notification for an item that waits on the person; never for routine news. */
export function notifyWaiting(it: WaitingItem, onClick: () => void) {
  if (!notifiable(it) || permission() !== 'granted') return
  try {
    const n = new Notification(announceText(it), { body: it.title, tag: `shepherd:${it.kind}:${it.decision_id ?? it.question_id ?? 0}` })
    n.onclick = () => {
      window.focus()
      onClick()
      n.close()
    }
  } catch {
    // A browser that refuses the constructor (some mobile ones) just gets no notification.
  }
}

/** The opt-in, off by default and turned on only by this click. It belongs in Settings once that page is free. */
export function NotifyToggle() {
  const { notify } = useSeen()
  const [perm, setPerm] = useState<Perm>(permission)
  if (perm === 'unsupported') return null
  const on = notify && perm === 'granted'
  const toggle = async () => {
    if (on) return seenStore.setNotify(false)
    const p = perm === 'default' ? await Notification.requestPermission() : perm
    setPerm(p)
    seenStore.setNotify(p === 'granted')
  }
  return (
    <span className="notify-toggle">
      <button type="button" className="button" aria-pressed={on} onClick={() => void toggle()} title="Browser notifications for new questions and decisions only">
        Notify me: {on ? 'on' : 'off'}
      </button>
      {perm === 'denied' && <span className="faint"> Blocked in this browser&rsquo;s site settings.</span>}
    </span>
  )
}
