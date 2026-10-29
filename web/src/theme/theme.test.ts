import { applyTheme, readTheme, setTheme, subscribeTheme, THEME_KEY } from './theme'

const attr = () => document.documentElement.getAttribute('data-theme')

describe('theme', () => {
  beforeEach(() => {
    window.localStorage.clear()
    document.documentElement.removeAttribute('data-theme')
  })

  it('reads system unless light or dark is stored', () => {
    expect(readTheme()).toBe('system')
    window.localStorage.setItem(THEME_KEY, 'dark')
    expect(readTheme()).toBe('dark')
    window.localStorage.setItem(THEME_KEY, 'mauve')
    expect(readTheme()).toBe('system')
  })

  it('forces light and dark, and leaves system to the OS', () => {
    applyTheme('dark')
    expect(attr()).toBe('dark')
    applyTheme('light')
    expect(attr()).toBe('light')
    applyTheme('system')
    expect(attr()).toBeNull()
  })

  it('remembers a choice and forgets it for system', () => {
    setTheme('light')
    expect(window.localStorage.getItem(THEME_KEY)).toBe('light')
    expect(attr()).toBe('light')
    setTheme('system')
    expect(window.localStorage.getItem(THEME_KEY)).toBeNull()
    expect(attr()).toBeNull()
  })

  it('still applies when storage throws', () => {
    const s = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('full')
    })
    setTheme('dark')
    expect(attr()).toBe('dark')
    s.mockRestore()
  })

  it('notifies subscribers and follows another tab', () => {
    const cb = vi.fn()
    const off = subscribeTheme(cb)
    setTheme('dark')
    expect(cb).toHaveBeenCalledTimes(1)
    window.localStorage.setItem(THEME_KEY, 'light')
    window.dispatchEvent(new StorageEvent('storage', { key: THEME_KEY }))
    expect(attr()).toBe('light')
    expect(cb).toHaveBeenCalledTimes(2)
    off()
  })
})
