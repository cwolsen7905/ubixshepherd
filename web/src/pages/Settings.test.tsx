import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ConfigChange, ConfigDoc } from '../api/types'
import { SettingsPage } from './Settings'

const doc = (o: Partial<ConfigDoc> = {}): ConfigDoc => ({
  path: '/h/config.yaml', etag: 'e1', can_write: true,
  raw: { repos: { api: { base_branch: 'dev' } } },
  effective: { daemon: { max_runs: 4 }, desk: {}, opencode: {} },
  defaults: { raw: {}, effective: { gate: 'make check', autonomy: { push: 'human' } }, overridden: [] },
  repos: { api: { raw: { base_branch: 'dev' }, effective: { gate: 'make check', base_branch: 'dev', autonomy: { push: 'human' } }, overridden: ['base_branch'] } },
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

describe('SettingsPage projects', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('adds a project and writes it with an empty repo list', async () => {
    const seen = daemon(withProjects(), (_m, p) => (p === '/v1/config/preview' ? json({ etag: 'e1', diff: '+  lab:\n+    repos: []\n' }) : undefined))
    const user = userEvent.setup()
    render(<SettingsPage scope="repo:api" />)
    await user.type(await screen.findByPlaceholderText('add a project'), 'lab{Enter}')
    expect(await screen.findByText('1 change')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Review changes' }))
    await screen.findByRole('region', { name: 'Review changes' })
    expect(seen.find((s) => s.path === '/v1/config/preview')?.body?.ops).toEqual([{ path: ['projects', 'lab', 'repos'], value: [] }])
  })

  it('refuses a project name that is not a plain key', async () => {
    daemon(withProjects())
    const user = userEvent.setup()
    render(<SettingsPage scope="repo:api" />)
    await user.type(await screen.findByPlaceholderText('add a project'), 'a b!{Enter}')
    expect(await screen.findByRole('alert')).toHaveTextContent('letters, digits')
  })

  it('shows which project each repo is in, and moves one with a single reviewed change', async () => {
    const seen = daemon(withProjects(), (_m, p) => (p === '/v1/config/preview' ? json({ etag: 'e1', diff: '-    repos: [api, web]\n+    repos: [web]\n' }) : undefined))
    const user = userEvent.setup()
    render(<SettingsPage scope="project:site" />)
    const picker = await screen.findByRole('group', { name: 'Repos' })
    expect(within(picker).getAllByText('in shop: ticking moves it here')).toHaveLength(2)
    expect(within(picker).getByText('in no project')).toBeInTheDocument()
    await user.click(within(picker).getByRole('checkbox', { name: 'api' }))
    expect(within(picker).getByRole('checkbox', { name: 'api' })).toBeChecked()
    await user.click(screen.getByRole('button', { name: 'Review changes' }))
    await screen.findByRole('region', { name: 'Review changes' })
    expect(seen.find((s) => s.path === '/v1/config/preview')?.body?.ops).toEqual([
      { path: ['projects', 'shop', 'repos'], value: ['web'] },
      { path: ['projects', 'site', 'repos'], value: ['api'] },
    ])
  })

  it('deletes a project only after confirming, and says its repos become unassigned', async () => {
    const seen = daemon(withProjects(), (_m, p) => (p === '/v1/config/preview' ? json({ etag: 'e1', diff: '-  shop: ...\n' }) : undefined))
    const user = userEvent.setup()
    render(<SettingsPage scope="project:shop" />)
    await user.click(await screen.findByRole('button', { name: 'Delete project…' }))
    const ask = screen.getByRole('group', { name: 'Delete project shop' })
    expect(ask).toHaveTextContent('api, web) become Unassigned')
    expect(screen.getByText('No changes')).toBeInTheDocument()
    await user.click(within(ask).getByRole('button', { name: 'Delete shop' }))
    await user.click(screen.getByRole('button', { name: 'Review changes' }))
    await screen.findByRole('region', { name: 'Review changes' })
    expect(seen.find((s) => s.path === '/v1/config/preview')?.body?.ops).toEqual([{ path: ['projects', 'shop'], unset: true }])
  })

  it('cannot add or delete in a session that may not save', async () => {
    daemon({ ...withProjects(), can_write: false, why: 'read-only' })
    render(<SettingsPage scope="project:shop" />)
    await screen.findByText(/read-only/)
    expect(screen.queryByPlaceholderText('add a project')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Delete project…' })).toBeNull()
  })
})

describe('SettingsPage', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('shows an inherited value greyed until it is overridden', async () => {
    daemon(doc())
    const user = userEvent.setup()
    render(<SettingsPage scope="repo:api" />)
    const gate = await screen.findByLabelText('Gate')
    expect(gate).toBeDisabled()
    expect(gate).toHaveValue('make check')
    expect(screen.getByText(/inherited: make check/)).toBeInTheDocument()
    await user.click(screen.getByLabelText('Override Gate'))
    expect(screen.getByLabelText('Gate')).toBeEnabled()
    await user.clear(screen.getByLabelText('Gate'))
    await user.type(screen.getByLabelText('Gate'), 'go test ./...')
    expect(screen.getByText('1 change')).toBeInTheDocument()
  })

  it('previews, then saves what was previewed, with the etag it read', async () => {
    const seen = daemon(doc(), (m, p) => {
      if (p === '/v1/config/preview') return json({ etag: 'e1', diff: '-    gate: a\n+    gate: b\n' })
      if (p === '/v1/config' && m === 'PUT') return json({ etag: 'e2', diff: '', reloaded: true })
      return undefined
    })
    const user = userEvent.setup()
    render(<SettingsPage scope="repo:api" />)
    await user.click(await screen.findByLabelText('Override Gate'))
    await user.type(screen.getByLabelText('Gate'), 'x')
    await user.click(screen.getByRole('button', { name: 'Review changes' }))
    const review = await screen.findByRole('region', { name: 'Review changes' })
    expect(within(review).getByLabelText('Diff of config.yaml')).toHaveTextContent('+ gate: b')
    await user.click(within(review).getByRole('button', { name: 'Save and reload' }))
    expect(await screen.findByText(/Saved and reloaded/)).toBeInTheDocument()
    const put = seen.find((s) => s.method === 'PUT')
    expect(put?.body).toMatchObject({ etag: 'e1', ops: [{ path: ['repos', 'api', 'gate'], value: 'make checkx' }] })
  })

  it('asks for the word before saving a change that gives agents more power', async () => {
    const seen = daemon(doc(), (m, p) => {
      if (p === '/v1/config/preview') {
        return json({ etag: 'e1', diff: '+    autonomy:\n+      push: agent\n', risky: [{ path: 'repos.api.autonomy.push', from: 'human', to: 'agent', why: 'lets agent act without the person' }] })
      }
      if (m === 'PUT') return json({ etag: 'e2', diff: '', reloaded: true })
      return undefined
    })
    const user = userEvent.setup()
    render(<SettingsPage scope="repo:api" />)
    await user.click(await screen.findByLabelText('Override Push'))
    await user.selectOptions(screen.getByLabelText('Push'), 'agent')
    await user.click(screen.getByRole('button', { name: 'Review changes' }))
    const review = await screen.findByRole('region', { name: 'Review changes' })
    expect(within(review).getByText(/give agents more power/)).toBeInTheDocument()
    const save = within(review).getByRole('button', { name: 'Save and reload' })
    expect(save).toBeDisabled()
    await user.type(within(review).getByRole('textbox'), 'confirm')
    expect(save).toBeEnabled()
    await user.click(save)
    await screen.findByText(/Saved and reloaded/)
    expect(seen.find((s) => s.method === 'PUT')?.body?.confirm).toEqual(['repos.api.autonomy.push'])
  })

  it('shows validation problems beside the field and does not offer to save', async () => {
    daemon(doc(), (_m, p) =>
      p === '/v1/config/preview'
        ? json({ error: 'invalid', problems: ['repos.api.autonomy.push: "robot" is not human, shepherd or agent'] }, 422)
        : undefined,
    )
    const user = userEvent.setup()
    render(<SettingsPage scope="repo:api" />)
    await user.click(await screen.findByLabelText('Override Push'))
    await user.click(screen.getByRole('button', { name: 'Review changes' }))
    const alerts = await screen.findAllByRole('alert')
    expect(alerts.some((a) => /robot/.test(a.textContent ?? ''))).toBe(true)
    expect(screen.getByRole('button', { name: 'Save and reload' })).toBeDisabled()
  })

  it('says the file changed on disk, and offers to reload it', async () => {
    daemon(doc(), (m, p) => {
      if (p === '/v1/config/preview') return json({ etag: 'e1', diff: '+x\n' })
      if (m === 'PUT') return json({ error: 'config.yaml changed on disk' }, 409)
      return undefined
    })
    const user = userEvent.setup()
    render(<SettingsPage scope="repo:api" />)
    await user.click(await screen.findByLabelText('Override Gate'))
    await user.click(screen.getByRole('button', { name: 'Review changes' }))
    await user.click(await screen.findByRole('button', { name: 'Save and reload' }))
    expect(await screen.findByText(/changed on disk since it was read/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Reload it' })).toBeInTheDocument()
  })

  it('tells a session that may not save, and offers no save', async () => {
    daemon(doc({ can_write: false, why: 'sign in again with shepherd web --allow-settings' }), (_m, p) =>
      p === '/v1/config/preview' ? json({ etag: 'e1', diff: '+x\n' }) : undefined,
    )
    const user = userEvent.setup()
    render(<SettingsPage scope="repo:api" />)
    expect(await screen.findByText(/--allow-settings/)).toBeInTheDocument()
    await user.click(screen.getByLabelText('Override Gate'))
    await user.click(screen.getByRole('button', { name: 'Review changes' }))
    await screen.findByRole('region', { name: 'Review changes' })
    expect(screen.queryByRole('button', { name: 'Save and reload' })).toBeNull()
  })
})
