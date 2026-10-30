import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { MoreMenu } from './MoreMenu'
import { PageMenuProvider, usePageMenu, usePageMenuItems } from './pageMenu'

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

  it('keeps a side conversation apart from the page, under its own heading', async () => {
    function Side() {
      usePageMenu([{ key: 'new', label: 'New conversation' }], 'Side conversation')
      return null
    }
    render(
      <PageMenuProvider>
        <MoreMenu />
        <Page onNew={() => {}} />
        <Side />
      </PageMenuProvider>,
    )
    await userEvent.click(screen.getByRole('button', { name: 'More' }))
    expect(screen.getByRole('group', { name: 'This page' })).toBeInTheDocument()
    expect(screen.getByRole('group', { name: 'Side conversation' })).toBeInTheDocument()
  })

  it('does not re-render the menu when nothing visible changed, and runs the latest onSelect', async () => {
    const calls: number[] = []
    function Counter() {
      const [n, setN] = useState(0)
      usePageMenu([{ key: 'a', label: 'Act', onSelect: () => calls.push(n) }])
      return <button type="button" onClick={() => setN(n + 1)}>bump</button>
    }
    let menuRenders = 0
    function Spy() {
      usePageMenuItems()
      menuRenders++
      return null
    }
    render(
      <PageMenuProvider>
        <MoreMenu />
        <Spy />
        <Counter />
      </PageMenuProvider>,
    )
    const before = menuRenders
    await userEvent.click(screen.getByRole('button', { name: 'bump' }))
    await userEvent.click(screen.getByRole('button', { name: 'bump' }))
    expect(menuRenders).toBe(before)
    await userEvent.click(screen.getByRole('button', { name: 'More' }))
    await userEvent.click(screen.getByRole('button', { name: 'Act' }))
    expect(calls).toEqual([2])
  })
})
