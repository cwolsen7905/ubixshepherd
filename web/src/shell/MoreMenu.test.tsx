import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MoreMenu } from './MoreMenu'
import { THEME_KEY } from '../theme/theme'

describe('MoreMenu', () => {
  beforeEach(() => {
    window.localStorage.clear()
    document.documentElement.removeAttribute('data-theme')
  })

  it('offers Dark, Light and System, with System chosen by default', async () => {
    render(<MoreMenu />)
    await userEvent.click(screen.getByRole('button', { name: 'More' }))
    expect(screen.getAllByRole('button', { pressed: undefined }).filter((b) => b.hasAttribute('aria-pressed')).map((r) => r.textContent?.replace('✓ ', ''))).toEqual(['System', 'Light', 'Dark'])
    expect(screen.getByRole('button', { name: 'System' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('applies and remembers a pick, and System clears it', async () => {
    render(<MoreMenu />)
    await userEvent.click(screen.getByRole('button', { name: 'More' }))
    await userEvent.click(screen.getByRole('button', { name: 'Dark' }))
    expect(document.documentElement).toHaveAttribute('data-theme', 'dark')
    expect(window.localStorage.getItem(THEME_KEY)).toBe('dark')
    expect(screen.getByRole('button', { name: 'Dark' })).toHaveAttribute('aria-pressed', 'true')
    await userEvent.click(screen.getByRole('button', { name: 'System' }))
    expect(document.documentElement).not.toHaveAttribute('data-theme')
    expect(window.localStorage.getItem(THEME_KEY)).toBeNull()
  })
})
