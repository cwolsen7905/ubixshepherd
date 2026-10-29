import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { MoreMenu } from './MoreMenu'
import { PageMenuProvider, usePageMenu } from './pageMenu'

function Page({ onNew }: { onNew: () => void }) {
  usePageMenu([{ key: 'new', label: 'New conversation', onSelect: onNew }])
  return <p>page</p>
}

function Host({ onNew }: { onNew: () => void }) {
  const [on, setOn] = useState(true)
  return (
    <PageMenuProvider>
      <MoreMenu />
      {on && <Page onNew={onNew} />}
      <button type="button" onClick={() => setOn(false)}>leave</button>
    </PageMenuProvider>
  )
}

describe('the one menu', () => {
  it('holds the theme always and a page items only while it is shown, under one button', async () => {
    const onNew = vi.fn()
    render(<Host onNew={onNew} />)
    expect(screen.getAllByRole('button', { name: 'More' })).toHaveLength(1)
    await userEvent.click(screen.getByRole('button', { name: 'More' }))
    expect(screen.getByRole('button', { name: 'Dark' })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'New conversation' }))
    expect(onNew).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('button', { name: 'New conversation' })).toBeNull()
    await userEvent.click(screen.getByRole('button', { name: 'leave' }))
    await userEvent.click(screen.getByRole('button', { name: 'More' }))
    expect(screen.queryByRole('button', { name: 'New conversation' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Light' })).toBeInTheDocument()
  })

  it('moves with j and k and closes on Escape, returning focus', async () => {
    render(<Host onNew={() => {}} />)
    const more = screen.getByRole('button', { name: 'More' })
    await userEvent.click(more)
    await userEvent.keyboard('j')
    expect(screen.getByRole('button', { name: 'System' })).toHaveFocus()
    await userEvent.keyboard('jjj')
    expect(screen.getByRole('button', { name: 'New conversation' })).toHaveFocus()
    await userEvent.keyboard('k')
    expect(screen.getByRole('button', { name: 'Dark' })).toHaveFocus()
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('group', { name: 'Menu' })).toBeNull()
    expect(more).toHaveFocus()
  })
})
