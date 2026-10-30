import { createContext, useContext, useEffect, useId, useMemo, useRef, useState, type ReactNode } from 'react'

/** One entry a page adds to the single "…" menu: a button, or a part the page draws itself. */
export type PageMenuItem = {
  key: string
  label?: string
  /** Shown as a toggle (aria-pressed) when set. */
  pressed?: boolean
  onSelect?: () => void
  /** A part the page draws itself, in place of the button. */
  node?: ReactNode
}

/** One page's items and the heading they sit under. */
export type PageMenuGroup = { title: string; items: readonly PageMenuItem[] }

type Entries = ReadonlyMap<string, PageMenuGroup>
type Setter = (id: string, group: PageMenuGroup | null) => void

const Items = createContext<Entries>(new Map())
const Set_ = createContext<Setter | null>(null)

/** Holds what pages register. The menu reads it; a page never renders its own menu button. */
export function PageMenuProvider({ children }: { children: ReactNode }) {
  const [entries, setEntries] = useState<Entries>(new Map())
  const set = useMemo<Setter>(
    () => (id, group) =>
      setEntries((prev) => {
        const next = new Map(prev)
        if (group === null || group.items.length === 0) next.delete(id)
        else next.set(id, group)
        return next
      }),
    [],
  )
  return (
    <Set_.Provider value={set}>
      <Items.Provider value={entries}>{children}</Items.Provider>
    </Set_.Provider>
  )
}

/**
 * A page calls this with its own actions. They join the one "…" menu under a divider and a
 * heading (`title`, "This page" unless a part of the page needs telling apart, such as a side
 * conversation) while the page is shown, and leave when it unmounts. Outside a shell it does
 * nothing. The menu is only told when a key, label, pressed state or the presence of a custom
 * part changes; `onSelect` always runs the latest one, and a custom `node` is kept as first drawn.
 */
export function usePageMenu(items: readonly PageMenuItem[], title = 'This page') {
  const id = useId()
  const set = useContext(Set_)
  const latest = useRef(items)
  useEffect(() => {
    latest.current = items
  })
  const shape = JSON.stringify(items.map((i) => [i.key, i.label, i.pressed, i.node !== undefined]))
  useEffect(() => {
    set?.(id, {
      title,
      items: latest.current.map((i) => ({ ...i, onSelect: i.onSelect && (() => latest.current.find((x) => x.key === i.key)?.onSelect?.()) })),
    })
  }, [id, set, title, shape])
  useEffect(() => () => set?.(id, null), [id, set])
}

/** What pages have registered, in the order they registered. */
export function usePageMenuItems(): readonly PageMenuGroup[] {
  return [...useContext(Items).values()]
}
