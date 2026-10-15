import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { Help } from './Help'

describe('Help', () => {
  it('lists the timeline shortcut', () => {
    render(<Help onClose={vi.fn()} />)

    const row = screen.getByText('Go to timeline').closest('div')
    expect(row?.querySelectorAll('kbd')).toHaveLength(2)
    expect(Array.from(row?.querySelectorAll('kbd') ?? []).map((key) => key.textContent)).toEqual(['g', 't'])
  })
})

describe('Help palette key', () => {
  it('lists the palette key', () => {
    render(<Help onClose={vi.fn()} />)
    expect(screen.getByText(/Search or act/)).toBeInTheDocument()
  })
})
