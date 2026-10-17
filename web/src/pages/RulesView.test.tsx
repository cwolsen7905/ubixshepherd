import { render, screen, within } from '@testing-library/react'
import type { ConfigDoc } from '../api/types'
import { RulesPage, rulesFrom } from './Rules'

const profile = (autonomy: Record<string, string>, overridden: string[] = []) => ({ raw: {}, effective: { autonomy }, overridden })

const doc = {
  defaults: profile({ push: 'human', merge: 'human', tag: 'human', deploy: 'human' }),
  projects: { shop: { raw: { autonomy: { merge: 'agent' } }, effective: {} } },
  repos: {
    'acme-api': profile({ push: 'agent', merge: 'agent', tag: 'human', deploy: 'human' }, ['autonomy.push', 'autonomy.merge']),
    web: profile({ push: 'human', merge: 'human', tag: 'agent', deploy: 'human' }, ['autonomy.tag']),
  },
} as unknown as ConfigDoc

describe('rulesFrom', () => {
  it('layers built-in, workspace, project and repo rules with their source', () => {
    const rules = rulesFrom(doc)
    expect(rules.filter((r) => r.layer === 'built-in')).toHaveLength(5)
    expect(rules.filter((r) => r.layer === 'workspace')).toHaveLength(4)
    expect(rules.filter((r) => r.layer === 'project').map((r) => r.source)).toEqual(['project shop'])
    const repo = rules.filter((r) => r.layer === 'repo')
    expect(repo.find((r) => r.id === 'repo:acme-api:merge')).toMatchObject({ holder: 'agent', source: 'repo acme-api' })
    // An inherited value is the workspace's, not the repo's own.
    expect(repo.find((r) => r.id === 'repo:web:merge')).toBeUndefined()
    expect(repo.find((r) => r.id === 'repo:web:tag')?.holder).toBe('agent')
    expect(repo.find((r) => r.id === 'repo:acme-api:tag')).toBeUndefined()
  })
})

describe('the Rules page', () => {
  it('shows the layers, and empty states for what waits on Crook', async () => {
    vi.stubGlobal('fetch', async () => new Response(JSON.stringify(doc), { status: 200 }))
    render(<RulesPage />)
    const repo = await screen.findByRole('region', { name: 'Repo' })
    expect(within(repo).getAllByText(/repo acme-api/, { selector: '.sh-row-sub' }).length).toBeGreaterThan(0)
    expect(screen.getByRole('region', { name: 'Proposed by Shepherd' })).toHaveTextContent('No proposed rules.')
    expect(screen.getByRole('region', { name: 'Shadow log' })).toHaveTextContent('The shadow log is empty.')
    vi.unstubAllGlobals()
  })
})
