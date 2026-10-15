import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('../state/context', () => ({
  useSnapshot: () => ({
    status: null,
    lanes: [],
    runs: [],
    decisions: [],
    waiting: null,
  }),
}))
vi.mock('../api/client', () => ({
  topics: { list: () => Promise.resolve({ topics: [] }) },
}))
vi.mock('../api/desk', () => ({
  deskApi: { newConversation: vi.fn(() => Promise.resolve({})) },
}))

import { deskApi } from '../api/desk'
import { CommandPalette } from './CommandPalette'

afterEach(() => {
  window.location.hash = ''
})

describe('CommandPalette', () => {
  it('opens on Ctrl+K and Cmd+K, even from an input', async () => {
    render(
      <>
        <input aria-label="elsewhere" />
        <CommandPalette />
      </>,
    )
    const field = screen.getByLabelText('elsewhere')
    field.focus()
    expect(screen.queryByRole('combobox')).toBeNull()
    await act(async () => void fireEvent.keyDown(field, { key: 'k', ctrlKey: true }))
    expect(screen.getByRole('combobox')).toBeInTheDocument()
    await act(async () => void fireEvent.keyDown(document.body, { key: 'k', metaKey: true }))
    expect(screen.queryByRole('combobox')).toBeNull()
  })

  it('goes to a page on Enter and closes', async () => {
    render(<CommandPalette />)
    await act(async () => void fireEvent.keyDown(document.body, { key: 'k', ctrlKey: true }))
    const box = screen.getByRole('combobox')
    fireEvent.change(box, { target: { value: 'timeline' } })
    fireEvent.keyDown(box, { key: 'Enter' })
    expect(window.location.hash).toBe('#/timeline')
    expect(screen.queryByRole('combobox')).toBeNull()
  })

  it('asks before starting a new conversation, then starts it', async () => {
    render(<CommandPalette />)
    await act(async () => void fireEvent.keyDown(document.body, { key: 'k', ctrlKey: true }))
    const box = screen.getByRole('combobox')
    fireEvent.change(box, { target: { value: 'new conv' } })
    fireEvent.keyDown(box, { key: 'Enter' })
    expect(deskApi.newConversation).not.toHaveBeenCalled()
    await act(
      async () =>
        void fireEvent.keyDown(screen.getByRole('alertdialog'), {
          key: 'Enter',
        }),
    )
    expect(deskApi.newConversation).toHaveBeenCalledWith(0)
    expect(window.location.hash).toBe('#/')
  })
})
