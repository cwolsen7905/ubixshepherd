import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { PageBack, PageHeader, PageTabs, pageTabIds } from './PageHeader'

function Page({ escape }: { escape?: 'close' | 'beside' }) {
  const [tab, setTab] = useState('log')
  return (
    <>
      <PageHeader
        back={<PageBack href="#/runs">← Runs</PageBack>}
        beside={{ label: 'Open beside the runs', onClick: () => window.dispatchEvent(new CustomEvent('beside')) }}
        onClose={() => window.dispatchEvent(new CustomEvent('closed'))}
        escape={escape}
        handleEscape
        glyph={<span>▸</span>}
        title="Run 17 running"
        meta={<span>acme-web</span>}
        actions={<button type="button">Dismiss</button>}
        danger={<button type="button">Close lane…</button>}
        tabs={<PageTabs tabs={[{ id: 'log', label: 'Log' }, { id: 'changes', label: <>Changes <span className="tab-count">8</span></> }]} value={tab} onChange={setTab} label="Run views" base="t" />}
      />
      <div role="tabpanel" id={pageTabIds('t', tab).panel} aria-labelledby={pageTabIds('t', tab).tab}>
        {tab}
      </div>
    </>
  )
}

describe('PageHeader', () => {
  it('has the top bar, the title row, the meta line, the actions and the tabs, in one header', () => {
    render(<Page />)
    const head = document.querySelector('[data-component="PageHeader"]') as HTMLElement
    expect(head.tagName).toBe('HEADER')
    const h = within(head)
    expect(h.getByRole('link', { name: '← Runs' })).toHaveAttribute('href', '#/runs')
    expect(h.getByRole('button', { name: 'Open beside the runs' })).toBeInTheDocument()
    expect(h.getByRole('button', { name: /^Close\s*Esc$/ })).toBeInTheDocument()
    expect(h.getByRole('heading', { level: 1, name: 'Run 17 running' })).toBeInTheDocument()
    expect(h.getByText('acme-web')).toBeInTheDocument()
    expect(h.getByRole('button', { name: 'Dismiss' })).toBeInTheDocument()
    expect(h.getByRole('button', { name: 'Close lane…' })).toBeInTheDocument()
    expect(h.getByRole('tablist', { name: 'Run views' })).toBeInTheDocument()
  })

  it('shows the tab counts as plain numbers, and names the panel the strip controls', async () => {
    render(<Page />)
    expect(screen.getByRole('tab', { name: 'Changes 8' }).querySelector('.tab-count')?.textContent).toBe('8')
    expect(screen.getByRole('tabpanel')).toHaveTextContent('log')
    await userEvent.click(screen.getByRole('tab', { name: /^Changes/ }))
    expect(screen.getByRole('tabpanel', { name: /Changes/ })).toHaveTextContent('changes')
    await userEvent.keyboard('{ArrowLeft}')
    expect(screen.getByRole('tab', { name: 'Log' })).toHaveAttribute('aria-selected', 'true')
  })

  it('closes on Esc, and puts the key chip on the button Esc answers', async () => {
    const closed = vi.fn()
    window.addEventListener('closed', closed)
    render(<Page />)
    await userEvent.keyboard('{Escape}')
    expect(closed).toHaveBeenCalledTimes(1)
    window.removeEventListener('closed', closed)
  })

  it('answers Esc with Open beside when the page says so', async () => {
    const beside = vi.fn()
    const closed = vi.fn()
    window.addEventListener('beside', beside)
    window.addEventListener('closed', closed)
    render(<Page escape="beside" />)
    expect(screen.getByRole('button', { name: /^Open beside the runs\s*Esc$/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Close' })).toBeInTheDocument()
    await userEvent.keyboard('{Escape}')
    expect(beside).toHaveBeenCalledTimes(1)
    expect(closed).not.toHaveBeenCalled()
    window.removeEventListener('beside', beside)
    window.removeEventListener('closed', closed)
  })

  it('leaves Esc to a field being typed in and to a dialog on screen', async () => {
    const closed = vi.fn()
    window.addEventListener('closed', closed)
    render(
      <>
        <Page />
        <input aria-label="note" />
      </>,
    )
    await userEvent.click(screen.getByLabelText('note'))
    await userEvent.keyboard('{Escape}')
    expect(closed).not.toHaveBeenCalled()
    const dlg = document.createElement('div')
    dlg.setAttribute('role', 'dialog')
    document.body.append(dlg)
    screen.getByLabelText('note').blur()
    await userEvent.keyboard('{Escape}')
    expect(closed).not.toHaveBeenCalled()
    dlg.remove()
    await userEvent.keyboard('{Escape}')
    expect(closed).toHaveBeenCalledTimes(1)
    window.removeEventListener('closed', closed)
  })
})
