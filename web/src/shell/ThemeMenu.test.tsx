import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ThemeMenu } from './ThemeMenu'
import { THEME_KEY } from '../theme/theme'

describe('ThemeMenu', () => {
  beforeEach(() => {
    window.localStorage.clear()
    document.documentElement.removeAttribute('data-theme')
  })

  it('offers Dark, Light and System, with System chosen by default', async () => {
    render(<ThemeMenu />)
    await userEvent.click(screen.getByRole('button', { name: 'More' }))
    expect(screen.getAllByRole('radio').map((r) => r.textContent?.replace('✓ ', ''))).toEqual(['System', 'Light', 'Dark'])
    expect(screen.getByRole('radio', { name: 'System' })).toHaveAttribute('aria-checked', 'true')
  })

  it('applies and remembers a pick, and System clears it', async () => {
    render(<ThemeMenu />)
    await userEvent.click(screen.getByRole('button', { name: 'More' }))
    await userEvent.click(screen.getByRole('radio', { name: 'Dark' }))
    expect(document.documentElement).toHaveAttribute('data-theme', 'dark')
    expect(window.localStorage.getItem(THEME_KEY)).toBe('dark')
    expect(screen.getByRole('radio', { name: 'Dark' })).toHaveAttribute('aria-checked', 'true')
    await userEvent.click(screen.getByRole('radio', { name: 'System' }))
    expect(document.documentElement).not.toHaveAttribute('data-theme')
    expect(window.localStorage.getItem(THEME_KEY)).toBeNull()
  })
})
