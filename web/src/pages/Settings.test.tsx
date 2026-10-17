import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ConfigChange, ConfigDoc } from '../api/types'
import { SettingsPage, THEME_KEY } from './Settings'

const doc = (o: Partial<ConfigDoc> = {}): ConfigDoc => ({
  path: '/h/config.yaml', etag: 'e1', can_write: true,
  raw: { repos: { api: { base_branch: 'dev' } } },
  effective: { daemon: { max_runs: 4, budget: 20, poll: '60s' }, keep_going: { cap: 5 }, desk: {}, chat: {}, opencode: {} },
  defaults: { raw: {}, effective: { gate: 'make check', autonomy: { push: 'human' }, agent: { model: { claude: 'anthropic/claude-sonnet-5' } } }, overridden: [] },
  repos: { api: { raw: { base_branch: 'dev' }, effective: { gate: 'make check', base_branch: 'dev', autonomy: { push: 'human', merge: 'human' } }, overridden: ['base_branch'] } },
  projects: {},
  sections: [
    { id: 'defaults', label: 'Defaults', root: ['defaults'], fields: [] },
    {
      id: 'repo', label: 'Repo profile', root: ['repos'], many: true, inherits: 'defaults',
      fields: [
        { key: ['gate'], kind: 'string', label: 'Gate', group: 'Checks' },
        { key: ['autonomy', 'push'], kind: 'enum', label: 'Push', group: 'Autonomy', choices: ['human', 'shepherd', 'agent'], risk: true },
      ],
    },
  ],
  ...o,
})

interface Seen { method: string; path: string; body?: ConfigChange }

/** A daemon with the given config; `answer` replaces what a preview or save returns. */
function daemon(d: ConfigDoc, answer: (m: string, p: string, b?: ConfigChange) => Response | undefined = () => undefined) {
  const seen: Seen[] = []
  vi.stubGlobal('fetch', async (path: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    const body = init?.body ? (JSON.parse(String(init.body)) as ConfigChange) : undefined
    seen.push({ method, path, ...(body ? { body } : {}) })
    if (path === '/v1/web/session') return new Response(JSON.stringify({ role: 'web', csrf: 'c' }))
    const custom = answer(method, path, body)
    if (custom) return custom
    if (path === '/v1/status?detail=1') return new Response(JSON.stringify({ version: '1', detail: { forge: { last_poll: new Date(Date.now() - 41000).toISOString() } } }))
    if (path === '/v1/config' && method === 'GET') return new Response(JSON.stringify(d))
    return new Response('{"error":"no"}', { status: 404 })
  })
  return seen
}

const json = (v: unknown, status = 200) => new Response(JSON.stringify(v), { status })

const withProjects = (): ConfigDoc =>
  doc({
    raw: { repos: { api: {}, web: {}, docs: {} }, projects: { shop: { repos: ['api', 'web'] }, site: { repos: [] } } },
    repos: {
      api: { raw: {}, effective: {}, overridden: [] }, web: { raw: {}, effective: {}, overridden: [] }, docs: { raw: {}, effective: {}, overridden: [] },
    },
    projects: {
      shop: { raw: { repos: ['api', 'web'] }, effective: { repos: ['api', 'web'] } },
      site: { raw: { repos: [] }, effective: { repos: [] } },
    },
    sections: [
      ...doc().sections,
      { id: 'project', label: 'Project', root: ['projects'], many: true, fields: [{ key: ['repos'], kind: 'list', label: 'Repos' }] },
    ],
  })



/** The ops of the latest preview the page asked for, once it has. */
const previewed = async (seen: Seen[]) => {
  await waitFor(() => expect(seen.some((s) => s.path === '/v1/config/preview')).toBe(true))
  return [...seen].reverse().find((s) => s.path === '/v1/config/preview')?.body?.ops
}

const diffOf = (lines: string) => json({ etag: 'e1', diff: lines })

describe('SettingsPage projects', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('adds a project and writes it with an empty repo list', async () => {
    const seen = daemon(withProjects(), (_m, p) => (p === '/v1/config/preview' ? diffOf('+  lab:\n+    repos: []\n') : undefined))
    const user = userEvent.setup()
    render(<SettingsPage scope="repo:api" />)
    await user.type(await screen.findByPlaceholderText('add a project'), 'lab{Enter}')
    expect(await screen.findByText('1 unsaved change')).toBeInTheDocument()
    expect(await previewed(seen)).toEqual([{ path: ['projects', 'lab', 'repos'], value: [] }])
  })

  it('refuses a project name that is not a plain key', async () => {
    daemon(withProjects())
    const user = userEvent.setup()
    render(<SettingsPage scope="repo:api" />)
    await user.type(await screen.findByPlaceholderText('add a project'), 'a b!{Enter}')
    expect(await screen.findByRole('alert')).toHaveTextContent('letters, digits')
  })

  it('shows which project each repo is in, and moves one with a single staged change', async () => {
    const seen = daemon(withProjects(), (_m, p) => (p === '/v1/config/preview' ? diffOf('-    repos: [api, web]\n+    repos: [web]\n') : undefined))
    const user = userEvent.setup()
    render(<SettingsPage scope="project:site" />)
    const picker = await screen.findByRole('group', { name: 'Repos' })
    expect(within(picker).getAllByText('in shop: ticking moves it here')).toHaveLength(2)
    expect(within(picker).getByText('in no project')).toBeInTheDocument()
    await user.click(within(picker).getByRole('checkbox', { name: 'api' }))
    expect(within(picker).getByRole('checkbox', { name: 'api' })).toBeChecked()
    expect(await previewed(seen)).toEqual([
      { path: ['projects', 'shop', 'repos'], value: ['web'] },
      { path: ['projects', 'site', 'repos'], value: ['api'] },
    ])
  })

  it('deletes a project only after confirming, and says its repos become unassigned', async () => {
    const seen = daemon(withProjects(), (_m, p) => (p === '/v1/config/preview' ? diffOf('-  shop: ...\n') : undefined))
    const user = userEvent.setup()
    render(<SettingsPage scope="project:shop" />)
    await user.click(await screen.findByRole('button', { name: 'Delete project…' }))
    const ask = screen.getByRole('group', { name: 'Delete project shop' })
    expect(ask).toHaveTextContent('api, web) become Unassigned')
    expect(screen.queryByRole('region', { name: 'Unsaved changes' })).toBeNull()
    await user.click(within(ask).getByRole('button', { name: 'Delete shop' }))
    expect(await previewed(seen)).toEqual([{ path: ['projects', 'shop'], unset: true }])
  })

  it('cannot add or delete in a session that may not save', async () => {
    daemon({ ...withProjects(), can_write: false, why: 'read-only' })
    render(<SettingsPage scope="project:shop" />)
    await screen.findByText(/read-only/)
    expect(screen.queryByPlaceholderText('add a project')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Delete project…' })).toBeNull()
  })
})

describe('SettingsPage sections', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    window.localStorage.clear()
    document.documentElement.removeAttribute('data-theme')
  })

  it('lists Budget, Agents, Forge, Merging, Appearance and Keyboard in one column', async () => {
    daemon(doc())
    render(<SettingsPage />)
    await screen.findByText('Daily cap')
    expect(screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent)).toEqual(['Budget', 'Agents', 'Forge', 'Merging', 'Appearance', 'Keyboard'])
    expect(screen.getByText('$20.00')).toBeInTheDocument()
    expect(screen.getByText('Parallel lanes')).toBeInTheDocument()
    // The model shows without its provider prefix, the full id in the title.
    expect(screen.getByText('claude-sonnet-5')).toHaveAttribute('title', 'anthropic/claude-sonnet-5')
    expect(await screen.findByText(/Polled \d+s ago · every 60s/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Merging for api: By a person' })).toHaveTextContent('By a person')
  })

  it('writes nothing before Save: staging previews only, Discard drops it', async () => {
    const seen = daemon(doc(), (_m, p) => (p === '/v1/config/preview' ? diffOf('-    budget: 20\n+    budget: 25\n') : undefined))
    const user = userEvent.setup()
    render(<SettingsPage />)
    await user.click(await screen.findByRole('button', { name: 'More daily cap' }))
    expect(screen.getByText('$25.00')).toBeInTheDocument()
    expect(screen.getByText('Not saved · was $20.00')).toBeInTheDocument()
    expect(await previewed(seen)).toEqual([{ path: ['daemon', 'budget'], value: 25 }])
    const diff = await screen.findByRole('region', { name: 'Diff of config.yaml' })
    expect(diff).toHaveTextContent('budget: 25')
    expect(seen.filter((s) => s.method === 'PUT')).toEqual([])
    await user.click(screen.getByRole('button', { name: 'Discard' }))
    expect(screen.queryByText(/Not saved/)).toBeNull()
    expect(screen.getByText('$20.00')).toBeInTheDocument()
    expect(seen.filter((s) => s.method === 'PUT')).toEqual([])
  })

  it('stepping back to the saved value stages nothing', async () => {
    daemon(doc())
    const user = userEvent.setup()
    render(<SettingsPage />)
    await user.click(await screen.findByRole('button', { name: 'More parallel lanes' }))
    expect(screen.getByText('1 unsaved change')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Less parallel lanes' }))
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it('saves what was previewed with the etag it read, after Save', async () => {
    const seen = daemon(doc(), (m, p) => {
      if (p === '/v1/config/preview') return diffOf('-    max_runs: 4\n+    max_runs: 5\n')
      if (p === '/v1/config' && m === 'PUT') return json({ etag: 'e2', diff: '', reloaded: true })
      return undefined
    })
    const user = userEvent.setup()
    render(<SettingsPage />)
    await user.click(await screen.findByRole('button', { name: 'More parallel lanes' }))
    const save = await screen.findByRole('button', { name: 'Save' })
    await waitFor(() => expect(save).toBeEnabled())
    await user.click(save)
    expect(await screen.findByText(/Saved and reloaded/)).toBeInTheDocument()
    expect(seen.find((s) => s.method === 'PUT')?.body).toMatchObject({ etag: 'e1', ops: [{ path: ['daemon', 'max_runs'], value: 5 }], confirm: [] })
  })

  it('marks a risky line with why, and confirms in the modal before it saves', async () => {
    const seen = daemon(doc(), (m, p) => {
      if (p === '/v1/config/preview') {
        return json({ etag: 'e1', diff: '+  repos:\n+    api:\n+      autonomy:\n+        merge: agent\n', risky: [{ path: 'repos.api.autonomy.merge', from: 'human', to: 'agent', why: 'lets an agent merge without the person' }] })
      }
      if (m === 'PUT') return json({ etag: 'e2', diff: '', reloaded: true })
      return undefined
    })
    const user = userEvent.setup()
    render(<SettingsPage />)
    await user.click(await screen.findByRole('button', { name: /^Merging for api/ }))
    await user.click(screen.getByRole('menuitemradio', { name: 'By an agent' }))
    expect(screen.getByText('Not saved · was By a person')).toBeInTheDocument()
    const diff = await screen.findByRole('region', { name: 'Diff of config.yaml' })
    await waitFor(() => expect(within(diff).getByText('Risky')).toBeInTheDocument())
    expect(within(diff).getByText('lets an agent merge without the person.')).toBeInTheDocument()
    expect(screen.getByText('1 risky')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Save…' }))
    const modal = await screen.findByRole('alertdialog')
    expect(modal).toHaveTextContent('repos.api.autonomy.merge')
    expect(seen.filter((s) => s.method === 'PUT')).toEqual([])
    await user.click(within(modal).getByRole('button', { name: 'Cancel' }))
    expect(seen.filter((s) => s.method === 'PUT')).toEqual([])
    await user.click(screen.getByRole('button', { name: 'Save…' }))
    await user.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Save and reload' }))
    await screen.findByText(/Saved and reloaded/)
    expect(seen.find((s) => s.method === 'PUT')?.body?.confirm).toEqual(['repos.api.autonomy.merge'])
  })

  it('marks the risky key under its own parent, not another section\'s key of the same name', async () => {
    daemon(doc(), (_m, p) =>
      p === '/v1/config/preview'
        ? json({ etag: 'e1', diff: '+  daemon:\n+    merge: x\n+  repos:\n+    api:\n+      autonomy:\n+        merge: agent\n', risky: [{ path: 'repos.api.autonomy.merge', from: 'human', to: 'agent', why: 'lets an agent merge' }] })
        : undefined,
    )
    const user = userEvent.setup()
    render(<SettingsPage />)
    await user.click(await screen.findByRole('button', { name: /^Merging for api/ }))
    await user.click(screen.getByRole('menuitemradio', { name: 'By an agent' }))
    const diff = await screen.findByRole('region', { name: 'Diff of config.yaml' })
    await waitFor(() => expect(within(diff).getAllByText(/Risky/)).toHaveLength(1))
    expect(within(diff).getByText('merge: agent').closest('.sh-diff-line')).toHaveTextContent('Risky')
    expect(within(diff).getByText('merge: x').closest('.sh-diff-line')).not.toHaveTextContent('Risky')
  })

  it('lists a risk whose parents the diff does not show, and marks no line for it', async () => {
    daemon(doc(), (_m, p) =>
      p === '/v1/config/preview'
        ? json({ etag: 'e1', diff: '+    merge: agent\n', risky: [{ path: 'repos.api.autonomy.merge', from: 'human', to: 'agent', why: 'lets an agent merge' }] })
        : undefined,
    )
    const user = userEvent.setup()
    render(<SettingsPage />)
    await user.click(await screen.findByRole('button', { name: /^Merging for api/ }))
    await user.click(screen.getByRole('menuitemradio', { name: 'By an agent' }))
    const list = await screen.findByRole('list', { name: 'Risky changes not shown on a line' })
    expect(list).toHaveTextContent('repos.api.autonomy.merge')
    expect(within(screen.getByRole('region', { name: 'Diff of config.yaml' })).getByText('merge: agent').closest('.sh-diff-line')).not.toHaveTextContent('Risky')
  })

  it('shows validation problems and offers no save', async () => {
    daemon(doc(), (_m, p) =>
      p === '/v1/config/preview' ? json({ error: 'invalid', problems: ['repos.api.autonomy.merge: "robot" is not human or agent'] }, 422) : undefined,
    )
    const user = userEvent.setup()
    render(<SettingsPage />)
    await user.click(await screen.findByRole('button', { name: /^Merging for api/ }))
    await user.click(screen.getByRole('menuitemradio', { name: 'By an agent' }))
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('robot')
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('says the file changed on disk, and offers to reload it', async () => {
    daemon(doc(), (m, p) => {
      if (p === '/v1/config/preview') return diffOf('+x\n')
      if (m === 'PUT') return json({ error: 'config.yaml changed on disk' }, 409)
      return undefined
    })
    const user = userEvent.setup()
    render(<SettingsPage />)
    await user.click(await screen.findByRole('button', { name: 'More parallel lanes' }))
    const save = await screen.findByRole('button', { name: 'Save' })
    await waitFor(() => expect(save).toBeEnabled())
    await user.click(save)
    expect(await screen.findByText(/changed on disk since it was read/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Reload it' })).toBeInTheDocument()
  })

  it('tells a session that may not save, and offers no Save', async () => {
    daemon(doc({ can_write: false, why: 'sign in again with shepherd web --allow-settings' }), (_m, p) => (p === '/v1/config/preview' ? diffOf('+x\n') : undefined))
    const user = userEvent.setup()
    render(<SettingsPage />)
    expect(await screen.findByText(/--allow-settings/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'More parallel lanes' }))
    await screen.findByRole('region', { name: 'Unsaved changes' })
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Save…' })).toBeNull()
  })

  it('applies the theme at once, outside Save, and remembers it in this browser', async () => {
    const seen = daemon(doc())
    const user = userEvent.setup()
    render(<SettingsPage />)
    await user.click(await screen.findByRole('button', { name: 'Dark' }))
    expect(document.documentElement).toHaveAttribute('data-theme', 'dark')
    expect(window.localStorage.getItem(THEME_KEY)).toBe('dark')
    expect(screen.queryByRole('region', { name: 'Unsaved changes' })).toBeNull()
    await user.click(screen.getByRole('button', { name: 'System' }))
    expect(document.documentElement).not.toHaveAttribute('data-theme')
    expect(seen.filter((s) => s.method !== 'GET')).toEqual([])
  })

  it('keeps the field editor under All settings: an inherited value greyed until overridden', async () => {
    daemon(doc())
    const user = userEvent.setup()
    render(<SettingsPage scope="repo:api" />)
    const gate = await screen.findByLabelText('Gate')
    expect(gate).toBeDisabled()
    expect(screen.getByText(/inherited: make check/)).toBeInTheDocument()
    expect(screen.getByText(/Raising this gives agents more power/)).toBeInTheDocument()
    await user.click(screen.getByLabelText('Override Gate'))
    await user.clear(screen.getByLabelText('Gate'))
    await user.type(screen.getByLabelText('Gate'), 'go test ./...')
    expect(screen.getByText('1 unsaved change')).toBeInTheDocument()
  })
})
