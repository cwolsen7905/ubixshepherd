import { createContext, useContext, useEffect, useId, useMemo, useState, type ReactNode } from 'react'

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

type Entries = ReadonlyMap<string, readonly PageMenuItem[]>
type Setter = (id: string, items: readonly PageMenuItem[] | null) => void

const Items = createContext<Entries>(new Map())
const Set_ = createContext<Setter | null>(null)

/** Holds what pages register. The menu reads it; a page never renders its own menu button. */
export function PageMenuProvider({ children }: { children: ReactNode }) {
  const [entries, setEntries] = useState<Entries>(new Map())
  const set = useMemo<Setter>(
    () => (id, items) =>
      setEntries((prev) => {
        const next = new Map(prev)
        if (items === null || items.length === 0) next.delete(id)
        else next.set(id, items)
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
 * A page calls this with its own actions. They join the one "…" menu under a divider while the
 * page is shown and leave when it unmounts. Outside a shell it does nothing.
 */
export function usePageMenu(items: readonly PageMenuItem[]) {
  const id = useId()
  const set = useContext(Set_)
  useEffect(() => {
    set?.(id, items)
  })
  useEffect(() => () => set?.(id, null), [id, set])
}

/** What pages have registered, in the order they registered. */
export function usePageMenuItems(): readonly (readonly PageMenuItem[])[] {
  return [...useContext(Items).values()]
}
