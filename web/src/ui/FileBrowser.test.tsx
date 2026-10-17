import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { FileBrowser } from './FileBrowser'

const tree = {
  '': { path: '', name: 'app', kind: 'dir', children: [
    { path: 'docs', name: 'docs', kind: 'dir' },
    { path: 'README.md', name: 'README.md', kind: 'file', size: 2048 },
    { path: 'logo.png', name: 'logo.png', kind: 'file', size: 10, binary: true },
  ] },
  docs: { path: 'docs', name: 'docs', kind: 'dir', children: [{ path: 'docs/plan.md', name: 'plan.md', kind: 'file', size: 7 }] },
  'README.md': { path: 'README.md', name: 'README.md', kind: 'file', size: 2048, data: '# hello\n', redacted: 1 },
  'logo.png': { path: 'logo.png', name: 'logo.png', kind: 'file', size: 10, binary: true },
} as const

let asked: string[] = []
beforeEach(() => {
  asked = []
  vi.stubGlobal('fetch', async (u: string) => {
    asked.push(u)
    const url = new URL(u, 'http://x')
    const node = (tree as Record<string, unknown>)[url.searchParams.get('path') ?? '']
    return node ? new Response(JSON.stringify(node)) : new Response(JSON.stringify({ error: 'file does not exist' }), { status: 404 })
  })
})
afterEach(() => vi.unstubAllGlobals())

describe('FileBrowser', () => {
  it('lists the root, opens a directory and goes back', async () => {
    render(<FileBrowser laneId={3} />)
    await userEvent.click(await screen.findByRole('button', { name: /docs/ }))
    expect(await screen.findByRole('button', { name: /plan\.md/ })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /back/ }))
    expect(await screen.findByRole('button', { name: /README\.md/ })).toBeInTheDocument()
  })

  it('reads a file beside the tree, saying how much was masked', async () => {
    render(<FileBrowser laneId={3} layout="tree-viewer" />)
    await userEvent.click(await screen.findByRole('button', { name: /README\.md/ }))
    expect(await screen.findByText(/# hello/)).toBeInTheDocument()
    expect(screen.getByText('1 masked')).toBeInTheDocument()
    expect(screen.getByRole('navigation', { name: 'Files' })).toBeInTheDocument()
  })

  it('says a binary file is not shown', async () => {
    render(<FileBrowser laneId={3} />)
    await userEvent.click(await screen.findByRole('button', { name: /logo\.png/ }))
    expect(await screen.findByText(/binary or too large/)).toBeInTheDocument()
  })

  it('shows only the tree in the tree layout, and reads a ref when given one', async () => {
    render(<FileBrowser laneId={3} layout="tree" gitRef="origin/dev" />)
    await userEvent.click(await screen.findByRole('button', { name: /README\.md/ }))
    await waitFor(() => expect(asked.some((u) => u.includes('path=README.md') && u.includes('ref=origin%2Fdev'))).toBe(true))
    expect(screen.queryByRole('article')).toBeNull()
    expect(document.querySelector('[data-component="FileBrowser"]')).toHaveAttribute('data-layout', 'tree')
  })

  it('says why the files could not be read', async () => {
    vi.stubGlobal('fetch', async () => new Response(JSON.stringify({ error: 'lane is closed' }), { status: 409 }))
    render(<FileBrowser laneId={3} />)
    expect(await screen.findByText(/Could not load the files: lane is closed/)).toBeInTheDocument()
  })
})
