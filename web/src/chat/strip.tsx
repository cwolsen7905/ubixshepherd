import { useCallback, useMemo } from 'react'
import type { WaitingItem } from '../api/types'
import { useKeys } from '../hooks'
import { neighbour, openKey, openTarget, requestStep, waitingKey } from '../model/waitingStep'
import { go, parse, withOpen } from '../router'
import { useSnapshot } from '../state/context'

const LABEL: Partial<Record<WaitingItem['kind'], string>> = {
  decision: 'Decision',
  question: 'Question',
  mr: 'Merge request',
  ship: 'To ship',
  next: 'Next up',
  request: 'Request',
}

/** Shows one waiting item: in the inspector when it has a pane, else in the thread. */
export function showWaiting(it: WaitingItem) {
  const target = openTarget(it)
  if (target) go(withOpen(window.location.hash, target))
  else requestStep(it)
}

/** Opens the item after the one just answered, or closes the inspector when nothing else waits. */
export function advanceFrom(items: readonly WaitingItem[], answered: string) {
  const next = neighbour(items, answered, 'down', answered)
  if (!next) go(withOpen(window.location.hash, undefined))
  else showWaiting(next)
}

/**
 * Above the composer: how many things wait on the person, each as a chip, with `]` and `[`
 * to step to the next and previous one. Stepping opens the item in the inspector and leaves
 * the thread where it is.
 */
export function PendingStrip() {
  const list = useSnapshot().waiting?.items
  const items = useMemo(() => list ?? [], [list])
  const at = openKey(items, parse(window.location.hash).open)
  const step = useCallback(
    (dir: 'up' | 'down') => {
      const it = neighbour(items, openKey(items, parse(window.location.hash).open), dir)
      if (it) showWaiting(it)
    },
    [items],
  )
  useKeys({ ']': () => step('down'), '[': () => step('up') }, [step])
  if (items.length === 0) return null
  return (
    <nav className="pending-strip" aria-label="Waiting on you">
      <span className="pending-count tone-warn">{items.length} waiting on you</span>
      <ul>
        {items.map((it) => {
          const key = waitingKey(it)
          return (
            <li key={key}>
              <button type="button" className="pending-chip" aria-current={key === at ? 'true' : undefined} onClick={() => showWaiting(it)}>
                <span className="faint">{LABEL[it.kind] ?? it.kind}</span> {it.title}
              </button>
            </li>
          )
        })}
      </ul>
      <span className="faint pending-keys">
        <kbd>[</kbd> <kbd>]</kbd> step
      </span>
    </nav>
  )
}
