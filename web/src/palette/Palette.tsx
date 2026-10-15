import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { Dialog, Kbd } from '../ui'
import { match, type Item } from './items'
import './palette.css'

/**
 * The palette's view: a combobox over a listbox in a dialog. Typing filters, the arrows move
 * the active option (focus stays in the input), Enter runs it, Esc closes and the dialog
 * returns focus to what had it. `confirm` swaps the list for a yes or no on one action.
 */
export function Palette(props: PaletteProps) {
  return (
    <Dialog open={props.open} onClose={props.onClose} title="Search or act">
      <PaletteBody {...props} />
    </Dialog>
  )
}

interface PaletteProps {
  open: boolean
  items: Item[]
  onClose: () => void
  onChoose: (item: Item) => void
  confirm?: { title: string; yes: string } | null
  onConfirm?: () => void
  onCancelConfirm?: () => void
  problem?: string
}

/** Mounted only while open, so the query and the active option start fresh each time. */
function PaletteBody({ items, onChoose, confirm, onConfirm, onCancelConfirm, problem }: PaletteProps) {
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const input = useRef<HTMLInputElement>(null)
  const listId = useId()
  const results = useMemo(() => match(items, query), [items, query])
  const at = Math.min(active, Math.max(results.length - 1, 0))

  useEffect(() => {
    if (!confirm) input.current?.focus()
  }, [confirm])

  const optionId = (i: number) => `${listId}-${i}`

  const onKey = (e: React.KeyboardEvent) => {
    if (confirm) {
      if (e.key === 'Enter') {
        e.preventDefault()
        onConfirm?.()
      } else if (e.key === 'Escape') {
        e.preventDefault()
        e.stopPropagation()
        onCancelConfirm?.()
      }
      return
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setActive(results.length ? (at + 1) % results.length : 0)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActive(results.length ? (at - 1 + results.length) % results.length : 0)
    } else if (e.key === 'Home') {
      e.preventDefault()
      setActive(0)
    } else if (e.key === 'End') {
      e.preventDefault()
      setActive(Math.max(results.length - 1, 0))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      const it = results[at]
      if (it) onChoose(it)
    } else if (e.key === 'Tab') {
      // Focus is held in the one input.
      e.preventDefault()
    }
  }

  useEffect(() => {
    document.getElementById(optionId(at))?.scrollIntoView?.({ block: 'nearest' })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [at, results])

  return (
    <div className="palette" onKeyDown={onKey}>
      {confirm ? (
        <div className="palette-confirm" role="alertdialog" aria-label={confirm.title}>
          <p>{confirm.title}</p>
          <p className="palette-keys">
            <Kbd keys="Enter" /> {confirm.yes} · <Kbd keys="Esc" /> back
          </p>
        </div>
      ) : (
        <>
          <input
            ref={input}
            className="palette-input"
            type="text"
            role="combobox"
            aria-expanded="true"
            aria-controls={listId}
            aria-autocomplete="list"
            aria-activedescendant={results.length ? optionId(at) : undefined}
            aria-label="Search pages, lanes, runs, decisions and actions"
            placeholder="Go to a page, or a lane, run or decision by name or number"
            autoComplete="off"
            spellCheck={false}
            value={query}
            onChange={(e) => {
              setQuery(e.target.value)
              setActive(0)
            }}
          />
          <ul id={listId} role="listbox" aria-label="Results" className="palette-list">
            {results.map((it, i) => (
              <li
                key={it.id}
                id={optionId(i)}
                role="option"
                aria-selected={i === at}
                className="palette-option"
                onMouseMove={() => i !== at && setActive(i)}
                onClick={() => onChoose(it)}
              >
                <span className="palette-group">{it.group}</span>
                <span className="palette-label">{it.label}</span>
                {it.hint && <span className="palette-hint">{it.hint}</span>}
              </li>
            ))}
          </ul>
          {results.length === 0 && (
            <p className="palette-empty" role="status">
              Nothing matches “{query}”.
            </p>
          )}
          <p className="palette-keys" aria-hidden="true">
            <Kbd keys={['↑', '↓']} /> move · <Kbd keys="Enter" /> open · <Kbd keys="Esc" /> close
          </p>
        </>
      )}
      {problem && (
        <p className="palette-problem" role="alert">
          {problem}
        </p>
      )}
    </div>
  )
}
