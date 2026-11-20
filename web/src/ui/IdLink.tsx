import { useState, type MouseEvent } from 'react'
import type { PrefixedId } from '../lib/ids'
import { go, refocus, withOpen } from '../router'
import './ui.css'

const NAME: Record<PrefixedId['kind'], string> = {
  question: 'question',
  decision: 'decision',
  next: 'next-up item',
  run: 'run',
  lane: 'lane',
  topic: 'topic',
}

/**
 * A prefixed ID in prose ("D-370") as a link that opens the thing in the side column,
 * on whatever page the person is on. It is a real anchor, so a middle click or copy of
 * the address still works. Inside another link (a row that is itself an anchor) or a
 * button it shows as a plain chip instead, since neither may hold an anchor. A row that is
 * only role="button" keeps the link, and the click stays the link's.
 */
export function IdLink({ to: r }: { to: PrefixedId }) {
  const [nested, setNested] = useState(false)
  const target = () => withOpen(window.location.hash, { kind: r.kind, id: r.id })
  const label = `${NAME[r.kind]} ${r.id}`
  const onClick = (e: MouseEvent<HTMLAnchorElement>) => {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
    e.preventDefault()
    // Inside a clickable row the click is the link's alone: the row must not select over it.
    e.stopPropagation()
    // A link to the hash already shown fires no hashchange: ask the page to focus again.
    const to = target()
    if (to.replace(/^#/, '') === window.location.hash.replace(/^#/, '')) refocus()
    else go(to)
  }
  // The anchor is swapped for a span once it is known to sit inside an anchor or a button, before paint.
  const seen = (a: HTMLAnchorElement | null) => {
    if (a?.parentElement?.closest('a, button')) setNested(true)
  }
  if (nested) {
    return (
      <span className="ui-idchip" data-component="IdLink" title={label}>
        {r.text}
      </span>
    )
  }
  return (
    <a ref={seen} className="ui-idchip" data-component="IdLink" href={target()} title={`Open ${label}`} onClick={onClick}>
      {r.text}
    </a>
  )
}
