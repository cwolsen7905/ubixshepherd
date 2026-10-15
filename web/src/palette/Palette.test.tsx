import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { buildItems } from './items'
import { Palette } from './Palette'
import { lane } from '../test/fixtures'

const items = buildItems({
  lanes: [lane({ id: 12, branch: 'feat/export-csv' })],
  runs: [],
  decisions: [],
  waiting: null,
})

function setup(over: Partial<React.ComponentProps<typeof Palette>> = {}) {
  const props = {
    open: true,
    items,
    onClose: vi.fn(),
    onChoose: vi.fn(),
    ...over,
  }
  render(<Palette {...props} />)
  return props
}

describe('Palette', () => {
  it('renders a combobox over a listbox', () => {
    setup()
    const box = screen.getByRole('combobox')
    expect(box).toHaveAttribute('aria-controls', screen.getByRole('listbox').id)
    expect(screen.getAllByRole('option').length).toBeGreaterThan(3)
    expect(box).toHaveAttribute('aria-activedescendant', screen.getAllByRole('option')[0]?.id)
  })

  it('renders nothing while closed', () => {
    setup({ open: false })
    expect(screen.queryByRole('combobox')).toBeNull()
  })

  it('filters, moves with the arrows and chooses with Enter', () => {
    const p = setup()
    const box = screen.getByRole('combobox')
    fireEvent.change(box, { target: { value: 'go to' } })
    fireEvent.keyDown(box, { key: 'ArrowDown' })
    const options = screen.getAllByRole('option')
    expect(options[1]).toHaveAttribute('aria-selected', 'true')
    fireEvent.keyDown(box, { key: 'Enter' })
    expect(p.onChoose).toHaveBeenCalledWith(expect.objectContaining({ label: expect.stringMatching(/^Go to/) }))
  })

  it('opens a lane by number', () => {
    const p = setup()
    const box = screen.getByRole('combobox')
    fireEvent.change(box, { target: { value: '12' } })
    fireEvent.keyDown(box, { key: 'Enter' })
    expect(p.onChoose).toHaveBeenCalledWith(expect.objectContaining({ id: 'lane:12' }))
  })

  it('says when nothing matches and does not choose', () => {
    const p = setup()
    const box = screen.getByRole('combobox')
    fireEvent.change(box, { target: { value: 'zzzz' } })
    expect(screen.getByRole('status')).toHaveTextContent('Nothing matches')
    fireEvent.keyDown(box, { key: 'Enter' })
    expect(p.onChoose).not.toHaveBeenCalled()
  })

  it('closes on Esc through the dialog', () => {
    const p = setup()
    fireEvent(screen.getByRole('dialog', { hidden: true }), new Event('cancel', { cancelable: true }))
    expect(p.onClose).toHaveBeenCalled()
  })

  it('confirms before a destructive action', () => {
    const onConfirm = vi.fn()
    setup({
      confirm: { title: 'Start a new conversation?', yes: 'start new' },
      onConfirm,
    })
    fireEvent.keyDown(screen.getByRole('alertdialog'), { key: 'Enter' })
    expect(onConfirm).toHaveBeenCalled()
  })
})
