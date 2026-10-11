import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { DiffFile, LaneDiff } from '../api/types'
import { Changes, lineClass } from './Changes'

const NOW = Date.parse('2026-10-11T10:05:00Z')

function file(over: Partial<DiffFile> & Pick<DiffFile, 'path'>): DiffFile {
  return { status: 'modified', added: 3, removed: 1, in_scope: true, ...over }
}

function read(over: Partial<LaneDiff> = {}): LaneDiff {
  return {
    lane_id: 4, lane: 'feat/x', branch: 'feat/x', base: 'dev', base_ref: 'origin/dev', head: 'abcdef1234567', mode: 'stat',
    read_at: '2026-10-11T10:04:00Z', commits_total: 1, out_of_scope: 1,
    commits: [{ sha: '1111111222222', subject: 'feat: add the thing', author: 'a', date: '2026-10-11T09:00:00Z', files: 2 }],
    committed: [file({ path: 'web/a.ts' }), file({ path: 'docs/out.md', in_scope: false, status: 'added', added: 9, removed: 0 })],
    uncommitted: [file({ path: 'web/b.ts' })],
    untracked: [file({ path: 'web/new.bin', status: 'untracked', binary: true, size: 2048, added: 0, removed: 0 })],
    ...over,
  }
}

const PATCH = 'diff --git a/web/a.ts b/web/a.ts\n--- a/web/a.ts\n+++ b/web/a.ts\n@@ -1,2 +1,3 @@\n keep\n-old\n+new\n+more\n'

function fakeFetch(r: LaneDiff, calls: string[]) {
  return async (path: string) => {
    calls.push(path)
    const u = new URL(path, 'http://x')
    const f = u.searchParams.get('file')
    const body = f ? { ...r, mode: 'file', file: f, patch: PATCH } : r
    return new Response(JSON.stringify(body), { status: 200 })
  }
}

function view(props: Partial<Parameters<typeof Changes>[0]> = {}) {
  return (
    <Changes laneId={4} worktree="/w/lanes/feat-x" editorUrl="vscode://file/{path}" refreshKey={1} now={NOW} {...props} />
  )
}

afterEach(() => vi.unstubAllGlobals())

describe('Changes', () => {
  it('lists commits and the three groups, marking a file outside the scope', async () => {
    const calls: string[] = []
    vi.stubGlobal('fetch', fakeFetch(read(), calls))
    render(view())
    expect(await screen.findByText('feat: add the thing')).toBeInTheDocument()
    expect(calls).toEqual(['/v1/lanes/4/diff?mode=stat'])
    const committed = screen.getByRole('region', { name: /Committed/ })
    expect(within(committed).getByText('web/a.ts')).toBeInTheDocument()
    expect(within(committed).getByText('outside scope')).toBeInTheDocument()
    expect(within(committed).getByText('+9')).toBeInTheDocument()
    expect(screen.getByRole('region', { name: /Uncommitted/ })).toHaveTextContent('web/b.ts')
    expect(screen.getByRole('region', { name: /Untracked/ })).toHaveTextContent('binary, 2048 bytes')
    expect(screen.getByText(/1 outside scope/)).toBeInTheDocument()
  })

  it('reads a file\'s patch only when it is opened, and draws unified diff lines', async () => {
    const calls: string[] = []
    vi.stubGlobal('fetch', fakeFetch(read(), calls))
    render(view())
    await userEvent.click(await screen.findByRole('button', { name: /web\/a\.ts/ }))
    const pre = await screen.findByLabelText('Patch of web/a.ts')
    expect(calls).toContain('/v1/lanes/4/diff?file=web%2Fa.ts')
    expect(within(pre).getByText('+new').className).toBe('patch-add')
    expect(within(pre).getByText('-old').className).toBe('patch-del')
    expect(within(pre).getByText('@@ -1,2 +1,3 @@').className).toBe('patch-hunk')
    expect(within(pre).getByText('+++ b/web/a.ts').className).toBe('patch-meta')
    await userEvent.click(screen.getByRole('button', { name: /web\/a\.ts/ }))
    expect(screen.queryByLabelText('Patch of web/a.ts')).not.toBeInTheDocument()
  })

  it('shows a binary file without asking for content', async () => {
    const calls: string[] = []
    vi.stubGlobal('fetch', fakeFetch(read(), calls))
    render(view())
    await userEvent.click(await screen.findByRole('button', { name: /new\.bin/ }))
    expect(screen.getByText(/Binary file, 2048 bytes: no content shown/)).toBeInTheDocument()
    expect(calls.filter((c) => c.includes('file='))).toEqual([])
  })

  it('reads again on Refresh and when the feed advances, but not on its own', async () => {
    const calls: string[] = []
    vi.stubGlobal('fetch', fakeFetch(read(), calls))
    const { rerender } = render(view())
    await screen.findByText('feat: add the thing')
    await userEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    await waitFor(() => expect(calls).toHaveLength(2))
    rerender(view({ refreshKey: 2 }))
    await waitFor(() => expect(calls).toHaveLength(3))
    rerender(view({ refreshKey: 2, now: NOW + 60000 }))
    expect(calls).toHaveLength(3)
  })

  it('links files and the worktree to the editor, and hides them when the template is empty', async () => {
    vi.stubGlobal('fetch', fakeFetch(read(), []))
    const { rerender } = render(view())
    await screen.findByText('feat: add the thing')
    const links = screen.getAllByRole('link', { name: /in editor/ })
    expect(links[0]).toHaveAttribute('href', 'vscode://file/w/lanes/feat-x/web/a.ts')
    rerender(view({ editorUrl: '' }))
    expect(screen.queryAllByRole('link', { name: /in editor/ })).toHaveLength(0)
  })

  it('says why a read failed and notes masked and cut answers', async () => {
    vi.stubGlobal('fetch', async () => new Response(JSON.stringify({ error: 'lane has no worktree on disk' }), { status: 409 }))
    render(view())
    expect(await screen.findByRole('alert')).toHaveTextContent('lane has no worktree on disk')
  })

  it('names what was masked or cut', async () => {
    vi.stubGlobal('fetch', fakeFetch(read({ redacted: 2, truncated: true, notes: ['200 files shown, 7 more: use --files'] }), []))
    render(view())
    expect(await screen.findByText(/2 spans masked/)).toBeInTheDocument()
    expect(screen.getByText(/7 more/)).toBeInTheDocument()
  })
})

describe('lineClass', () => {
  it('tells headers from content', () => {
    expect(lineClass('+++ b/x')).toBe('patch-meta')
    expect(lineClass('+added')).toBe('patch-add')
    expect(lineClass('--- a/x')).toBe('patch-meta')
    expect(lineClass('-gone')).toBe('patch-del')
    expect(lineClass(' same')).toBe('patch-ctx')
  })
})
