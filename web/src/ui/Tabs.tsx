import { useId, useRef, type KeyboardEvent, type ReactNode } from 'react'
import { cx } from './cx'

export type TabItem = { id: string; label: ReactNode; content: ReactNode }

/**
 * Controlled tabs: the caller owns `value`, so it can live in the address. Arrow keys, Home and
 * End move between tabs; only the selected tab is a tab stop.
 */
export function Tabs({
  items,
  value,
  onChange,
  label,
}: {
  items: TabItem[]
  value: string
  onChange: (id: string) => void
  label: string
}) {
  const base = useId()
  const refs = useRef(new Map<string, HTMLButtonElement>())
  const current = items.find((t) => t.id === value) ?? items[0]

  function onKey(e: KeyboardEvent, i: number) {
    const last = items.length - 1
    const to =
      e.key === 'ArrowRight' ? (i === last ? 0 : i + 1)
      : e.key === 'ArrowLeft' ? (i === 0 ? last : i - 1)
      : e.key === 'Home' ? 0
      : e.key === 'End' ? last
      : -1
    if (to < 0) return
    e.preventDefault()
    const next = items[to]
    if (!next) return
    onChange(next.id)
    refs.current.get(next.id)?.focus()
  }

  return (
    <div data-component="Tabs">
      <div role="tablist" aria-label={label} className="sh-tabs ui-tabs-list">
        {items.map((t, i) => (
          <button
            key={t.id}
            ref={(el) => {
              if (el) refs.current.set(t.id, el)
              else refs.current.delete(t.id)
            }}
            type="button"
            role="tab"
            id={`${base}-tab-${t.id}`}
            aria-selected={t.id === current?.id}
            aria-controls={`${base}-panel-${t.id}`}
            tabIndex={t.id === current?.id ? 0 : -1}
            className={cx('sh-tab', t.id === current?.id && 'is-on', 'ui-tab')}
            onClick={() => onChange(t.id)}
            onKeyDown={(e) => onKey(e, i)}
          >
            {t.label}
          </button>
        ))}
      </div>
      {current && (
        <div role="tabpanel" id={`${base}-panel-${current.id}`} aria-labelledby={`${base}-tab-${current.id}`} className="sh-tabpanel">
          {current.content}
        </div>
      )}
    </div>
  )
}
