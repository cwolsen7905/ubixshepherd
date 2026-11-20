import { useState, type MouseEvent } from 'react'
import type { PrefixedId } from '../lib/ids'
import { go, withOpen } from '../router'
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
 * the address still works. Inside another link (a row that is itself an anchor) it shows
 * as a plain chip instead, since an anchor may not nest in an anchor.
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
    go(target())
  }
  // The anchor is swapped for a span once it is known to sit inside another anchor, before paint.
  const seen = (a: HTMLAnchorElement | null) => {
    if (a?.parentElement?.closest('a')) setNested(true)
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
