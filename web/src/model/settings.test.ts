import type { ConfigDoc, ConfigField } from '../api/types'
import {
  deletedProjects, fieldState, mapText, moveRepo, opsOf, ownerOf, parseList, parseMap, problemsFor, projectRepos, removeProject, revert, scopes, setValue, unsetValue,
} from './settings'

const doc: ConfigDoc = {
  path: '/h/config.yaml', etag: 'e1', can_write: true,
  raw: { defaults: { gate: 'make check' }, repos: { api: { base_branch: 'dev' } } },
  effective: { daemon: { max_runs: 4 }, desk: {}, chat: {}, opencode: {} },
  defaults: { raw: { gate: 'make check' }, effective: { gate: 'make check', base_branch: 'main' }, overridden: ['gate'] },
  repos: { api: { raw: { base_branch: 'dev' }, effective: { gate: 'make check', base_branch: 'dev' }, overridden: ['base_branch'] } },
  projects: {},
  sections: [
    { id: 'daemon', label: 'Daemon', root: ['daemon'], fields: [{ key: ['max_runs'], kind: 'number', label: 'Max runs' }] },
    { id: 'defaults', label: 'Defaults', root: ['defaults'], fields: [{ key: ['gate'], kind: 'string', label: 'Gate' }] },
    {
      id: 'repo', label: 'Repo profile', root: ['repos'], many: true, inherits: 'defaults',
      fields: [{ key: ['gate'], kind: 'string', label: 'Gate' }, { key: ['base_branch'], kind: 'string', label: 'Base branch' }],
    },
  ],
}

describe('settings model', () => {
  it('lists each place that can be edited, repos from the file and the ones just added', () => {
    expect(scopes(doc, ['web']).map((s) => s.id)).toEqual(['defaults', 'repo:api', 'repo:web', 'daemon'])
  })

  it('reads a field as overridden or inherited, and a draft over both', () => {
    const repo = scopes(doc).find((s) => s.id === 'repo:api')
    if (!repo) throw new Error('no repo scope')
    const [gate, base] = repo.section.fields as [ConfigField, ConfigField]
    expect(fieldState(repo, gate, {})).toEqual({ set: false, value: 'make check', dirty: false })
    expect(fieldState(repo, base, {})).toEqual({ set: true, value: 'dev', dirty: false })
    let d = setValue({}, ['repos', 'api', 'gate'], 'go test')
    expect(fieldState(repo, gate, d)).toEqual({ set: true, value: 'go test', dirty: true })
    d = unsetValue({}, ['repos', 'api', 'base_branch'])
    expect(fieldState(repo, base, d)).toMatchObject({ set: false, dirty: true })
    expect(opsOf(d)).toEqual([{ path: ['repos', 'api', 'base_branch'], unset: true }])
    expect(opsOf(revert(d, ['repos', 'api', 'base_branch']))).toEqual([])
  })

  it('keeps one change per path', () => {
    let d = setValue({}, ['daemon', 'max_runs'], 5)
    d = setValue(d, ['daemon', 'max_runs'], 6)
    expect(opsOf(d)).toEqual([{ path: ['daemon', 'max_runs'], value: 6 }])
  })

  it('turns lists and maps to text and back', () => {
    expect(parseList(' a \n\nb ')).toEqual(['a', 'b'])
    expect(parseMap('claude: sonnet\nbad line\ncursor: gpt: 5')).toEqual({ claude: 'sonnet', cursor: 'gpt: 5' })
    expect(mapText({ claude: 'sonnet' })).toBe('claude: sonnet')
  })

  it('matches a problem to its field by path', () => {
    const ps = ['repos.api.autonomy.push: "robot" is not human, shepherd or agent', 'repos.apix.gate: empty']
    expect(problemsFor(ps, 'repos.api.autonomy.push')).toHaveLength(1)
    expect(problemsFor(ps, 'repos.api')).toHaveLength(1)
  })
})

describe('projects', () => {
  const pdoc: ConfigDoc = {
    ...doc,
    projects: { shop: { raw: { repos: ['api', 'web'] }, effective: {} }, site: { raw: { repos: ['docs'] }, effective: {} } },
    sections: [...doc.sections, { id: 'project', label: 'Project', root: ['projects'], many: true, fields: [] }],
  }

  it('lists the projects of the file and the ones just added', () => {
    expect(scopes(pdoc, [], ['lab']).map((s) => s.id).filter((i) => i.startsWith('project:'))).toEqual(['project:lab', 'project:shop', 'project:site'])
  })

  it('moves a repo from the project that holds it, changing both lists once', () => {
    const d = moveRepo(pdoc, {}, [], 'site', 'api', true)
    expect(opsOf(d)).toEqual([
      { path: ['projects', 'shop', 'repos'], value: ['web'] },
      { path: ['projects', 'site', 'repos'], value: ['docs', 'api'] },
    ])
    const map = projectRepos(pdoc, d)
    expect(ownerOf(map, 'api')).toBe('site')
    expect(map.get('shop')).toEqual(['web'])
  })

  it('takes a repo out without touching others', () => {
    const d = moveRepo(pdoc, {}, [], 'shop', 'web', false)
    expect(opsOf(d)).toEqual([{ path: ['projects', 'shop', 'repos'], value: ['api'] }])
    expect(ownerOf(projectRepos(pdoc, d), 'web')).toBeUndefined()
  })

  it('deletes a project, dropping its drafted edits, and its repos become unowned', () => {
    let d = moveRepo(pdoc, {}, [], 'shop', 'docs', true)
    d = removeProject(pdoc, d, 'shop')
    expect(opsOf(d)).toEqual([
      { path: ['projects', 'site', 'repos'], value: [] },
      { path: ['projects', 'shop'], unset: true },
    ])
    expect([...deletedProjects(d)]).toEqual(['shop'])
    const map = projectRepos(pdoc, d)
    expect([...map.keys()]).toEqual(['site'])
    expect(ownerOf(map, 'api')).toBeUndefined()
  })

  it('forgets a project that was only drafted', () => {
    const d = removeProject(pdoc, setValue({}, ['projects', 'lab', 'repos'], []), 'lab')
    expect(opsOf(d)).toEqual([])
  })
})
