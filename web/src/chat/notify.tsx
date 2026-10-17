import { useState } from 'react'
import type { WaitingItem } from '../api/types'
import { announceText, notifiable, seenStore, useSeen } from '../state/seen'

type Perm = NotificationPermission | 'unsupported'
const permission = (): Perm => (typeof Notification === 'undefined' ? 'unsupported' : Notification.permission)

/** True when a notification may be shown now: the person opted in, the browser agrees. */
export const mayNotify = (on: boolean) => on && permission() === 'granted'

/** Shows one browser notification for what newly waits on the person (a count when several); never for routine news. */
export function notifyWaiting(items: readonly WaitingItem[], onClick: () => void) {
  const waits = items.filter(notifiable)
  const [first] = waits
  if (!first || permission() !== 'granted') return
  try {
    const one = waits.length === 1
    const n = new Notification(one ? announceText(first) : `${waits.length} new items waiting on you`, {
      body: one ? first.title : waits.map((w) => w.title).join(', '),
      tag: 'shepherd:waiting',
    })
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
