/** The theme choice for this browser: Dark, Light, or System (follow the OS, live). */
export type Theme = 'system' | 'light' | 'dark'
// The same key Settings writes, so the two agree.
export const THEME_KEY = 'sh-theme'
export const THEMES: [Theme, string][] = [['system', 'System'], ['light', 'Light'], ['dark', 'Dark']]

export function readTheme(): Theme {
  try {
    const v = window.localStorage.getItem(THEME_KEY)
    return v === 'light' || v === 'dark' ? v : 'system'
  } catch {
    return 'system'
  }
}

/**
 * System leaves the choice to the OS: tokens.css follows prefers-color-scheme on its own, so the
 * attribute is removed and a change in the OS preference applies live. Light and dark force it.
 */
export function applyTheme(t: Theme) {
  if (t === 'system') document.documentElement.removeAttribute('data-theme')
  else document.documentElement.setAttribute('data-theme', t)
}

const listeners = new Set<() => void>()

/** Choose a theme: apply it now and remember it. A browser that will not store still gets it for this page. */
export function setTheme(t: Theme) {
  applyTheme(t)
  try {
    if (t === 'system') window.localStorage.removeItem(THEME_KEY)
    else window.localStorage.setItem(THEME_KEY, t)
  } catch {
    /* private mode or full storage: the choice holds until reload */
  }
  listeners.forEach((l) => l())
}

/** For useSyncExternalStore: changes here, and in other tabs through the storage event. */
export function subscribeTheme(cb: () => void) {
  listeners.add(cb)
  const onStorage = (e: StorageEvent) => {
    if (e.key !== THEME_KEY && e.key !== null) return
    applyTheme(readTheme())
    cb()
  }
  window.addEventListener('storage', onStorage)
  return () => {
    listeners.delete(cb)
    window.removeEventListener('storage', onStorage)
  }
}

// Before first paint index.html sets the attribute inline; this covers a page without that script.
if (typeof document !== 'undefined') applyTheme(readTheme())
