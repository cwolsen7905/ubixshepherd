import { useSyncExternalStore } from 'react'
import { Popover } from '../ui/Popover'
import { readTheme, setTheme, subscribeTheme, THEMES } from '../theme/theme'
import './ThemeMenu.css'

/** The ⋯ menu at the top right: Dark, Light or System, remembered for this browser. */
export function ThemeMenu() {
  const theme = useSyncExternalStore(subscribeTheme, readTheme)
  return (
    <div className="sh-theme-menu" data-component="ThemeMenu">
      <Popover label={<span aria-label="More">⋯</span>}>
        <div role="radiogroup" aria-label="Theme" className="sh-theme-options">
          {THEMES.map(([value, name]) => (
            <button
              key={value}
              type="button"
              role="radio"
              aria-checked={theme === value}
              className="sh-btn sh-btn--ghost"
              onClick={() => setTheme(value)}
            >
              {name}
            </button>
          ))}
        </div>
      </Popover>
    </div>
  )
}
