import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { LaneDiff } from '../api/types'
import { ChangesList } from './ChangesList'

const file = (path: string, over = {}) => ({ path, status: 'modified', added: 3, removed: 1, in_scope: true, ...over })
const read: LaneDiff = {
  lane_id: 1, lane: 'feat/login', branch: 'feat/login', base: 'dev', base_ref: 'origin/dev', head: 'abcdef1234567', mode: 'stat',
  read_at: '2026-10-08T10:04:00Z',
  commits: [{ sha: 'a1b2c3d4e5f6', subject: 'Fix the redirect', author: 'a', date: '', files: 2 }], commits_total: 1,
  committed: [file('src/auth.go'), file('other/x.go', { in_scope: false })],
  uncommitted: [file('README.md')],
  untracked: [file('notes.txt', { status: 'untracked', size: 12 })],
  out_of_scope: 1,
}

describe('ChangesList', () => {
  it('shows the commits and the three sections, and marks files outside scope', () => {
    render(<ChangesList read={read} worktree="/w" editorUrl={null} variant="list" />)
    expect(screen.getByText('Fix the redirect')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /Committed/ })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /Uncommitted/ })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /Untracked/ })).toBeInTheDocument()
    expect(screen.getByText('1 outside scope')).toBeInTheDocument()
    const row = screen.getByText('other/x.go').closest('li') as HTMLElement
    expect(within(row).getByText('outside scope')).toBeInTheDocument()
    expect(document.querySelector('[data-component="ChangesList"]')).toHaveAttribute('data-variant', 'list')
  })

  it('list: rows do not expand', () => {
    render(<ChangesList read={read} worktree="/w" editorUrl={null} variant="list" />)
    expect(screen.queryByRole('button', { name: /src\/auth\.go/ })).toBeNull()
  })

  it('diff: a file opens to its patch', async () => {
    vi.stubGlobal('fetch', async () => new Response(JSON.stringify({ ...read, file: 'src/auth.go', patch: '@@ -1 +1 @@\n-old\n+new\n' })))
    render(<ChangesList read={read} worktree="/w" editorUrl={null} variant="diff" />)
    await userEvent.click(screen.getByRole('button', { name: /src\/auth\.go/ }))
    expect(await screen.findByText('+new')).toBeInTheDocument()
    expect(screen.getByText('-old')).toHaveClass('patch-del')
    vi.unstubAllGlobals()
  })

  it('narrowed to a run, only the commits are that run\'s and later work is left out', () => {
    render(<ChangesList read={read} worktree="/w" editorUrl={null} run={{ id: 5, latest: false }} variant="list" />)
    expect(screen.getByText(/Run 5:/)).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: /Uncommitted/ })).toBeNull()
  })

  it('links to the editor only when a template is set', () => {
    const { rerender } = render(<ChangesList read={read} worktree="/w" editorUrl={null} />)
    expect(screen.queryByRole('link', { name: /in editor/ })).toBeNull()
    rerender(<ChangesList read={read} worktree="/w" editorUrl="cursor://file/{path}" />)
    expect(screen.getAllByRole('link', { name: /in editor/ })[0]).toHaveAttribute('href', expect.stringMatching(/^cursor:\/\/file\//))
  })
})
