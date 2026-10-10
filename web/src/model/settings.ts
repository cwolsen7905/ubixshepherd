// The settings form's logic, apart from its markup: where a field's value lives, what
// the draft changes, and how a refusal reads.
import type { ConfigDoc, ConfigField, ConfigOp, ConfigSection } from '../api/types'

/** A place the form edits: a section, and the entry's name for the ones keyed by name. */
export interface Scope {
  /** The address's name for it: "daemon", "defaults", "repo:api", "project:web". */
  id: string
  label: string
  section: ConfigSection
  /** Where its keys start in the file. */
  root: string[]
  raw: Record<string, unknown>
  /** Values in force: what an unset field shows. */
  effective: Record<string, unknown>
  /** An entry of a section that inherits shows its unset fields as inherited. */
  inherits: boolean
}

export function getAt(obj: unknown, path: string[]): unknown {
  let cur = obj
  for (const k of path) {
    if (cur === null || typeof cur !== 'object' || Array.isArray(cur)) return undefined
    cur = (cur as Record<string, unknown>)[k]
  }
  return cur
}

const asMap = (v: unknown): Record<string, unknown> => (v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : {})

/** Every place the form can edit, in the order the sidebar lists them. */
export function scopes(doc: ConfigDoc, extraRepos: string[] = [], extraProjects: string[] = []): Scope[] {
  const by = new Map(doc.sections.map((s) => [s.id, s]))
  const out: Scope[] = []
  const one = (id: string, effective: Record<string, unknown>, raw: Record<string, unknown>) => {
    const section = by.get(id)
    if (section) out.push({ id, label: section.label, section, root: section.root, raw, effective, inherits: false })
  }
  one('defaults', doc.defaults.effective, doc.defaults.raw)
  const repo = by.get('repo')
  if (repo) {
    const names = [...new Set([...Object.keys(doc.repos), ...extraRepos])].sort()
    for (const name of names) {
      const p = doc.repos[name]
      out.push({
        id: `repo:${name}`, label: name, section: repo, root: [...repo.root, name],
        raw: p?.raw ?? {}, effective: p?.effective ?? doc.defaults.effective, inherits: true,
      })
    }
  }
  const project = by.get('project')
  if (project) {
    for (const name of [...new Set([...Object.keys(doc.projects), ...extraProjects])].sort()) {
      const p = doc.projects[name]
      out.push({ id: `project:${name}`, label: name, section: project, root: [...project.root, name], raw: asMap(p?.raw), effective: asMap(p?.effective), inherits: false })
    }
  }
  for (const id of ['daemon', 'desk', 'opencode']) one(id, asMap(doc.effective[id]), asMap(doc.raw[id]))
  return out
}

/** The draft: what the person changed and has not saved, by full path. */
export type Draft = Record<string, ConfigOp>

const keyOf = (path: string[]) => path.join('\u0000')
export const dotted = (path: string[]) => path.join('.')

export function setValue(d: Draft, path: string[], value: unknown): Draft {
  return { ...d, [keyOf(path)]: { path, value } }
}

export function unsetValue(d: Draft, path: string[]): Draft {
  return { ...d, [keyOf(path)]: { path, unset: true } }
}

/** Drop the draft's change for a path: back to what the file says. */
export function revert(d: Draft, path: string[]): Draft {
  const { [keyOf(path)]: _gone, ...rest } = d
  void _gone
  return rest
}

export const opsOf = (d: Draft): ConfigOp[] => Object.values(d)

/** What a field reads as now: the draft's value, else the file's, else the value in force. */
export interface FieldState {
  /** Whether the file (as drafted) sets it. */
  set: boolean
  value: unknown
  /** Whether the draft changes it. */
  dirty: boolean
}

export function fieldState(scope: Scope, f: ConfigField, draft: Draft): FieldState {
  const path = [...scope.root, ...f.key]
  const op = draft[keyOf(path)]
  if (op) return op.unset ? { set: false, value: getAt(scope.effective, f.key), dirty: true } : { set: true, value: op.value, dirty: true }
  const raw = getAt(scope.raw, f.key)
  if (raw !== undefined) return { set: true, value: raw, dirty: false }
  return { set: false, value: getAt(scope.effective, f.key), dirty: false }
}

/** The text a list or map is edited as, and back. */
export const listText = (v: unknown) => (Array.isArray(v) ? v.map(String).join('\n') : '')
export const parseList = (s: string) => s.split('\n').map((l) => l.trim()).filter(Boolean)
export const mapText = (v: unknown) =>
  Object.entries(asMap(v)).map(([k, x]) => `${k}: ${String(x)}`).join('\n')
export function parseMap(s: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const line of s.split('\n')) {
    const i = line.indexOf(':')
    if (i < 0) continue
    const k = line.slice(0, i).trim()
    if (k) out[k] = line.slice(i + 1).trim()
  }
  return out
}

/** Problems that name a path (or one under it), for showing beside its field. */
export function problemsFor(problems: string[], full: string): string[] {
  return problems.filter((p) => p.startsWith(full + ':') || p.startsWith(full + '.') || p.startsWith(full + ' '))
}

/** The scope a hash names, or the first. */
export function pickScope(all: Scope[], id: string | undefined): Scope | undefined {
  return all.find((s) => s.id === id) ?? all[0]
}

/** How a displayed value reads in a box, a hint or a diff line. */
export function show(v: unknown): string {
  if (v === undefined || v === null || v === '') return '(empty)'
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}

// Projects: a repo is in at most one, so the picker moves a repo by editing two lists in
// one draft and the person reviews both together.

/** The scope the Projects page's "New project" button names: Settings with the add box focused. */
export const NEW_PROJECT = 'new-project'

/** Whether a name can be a project's key: what a YAML key can safely be here. */
export const PROJECT_NAME = /^[A-Za-z0-9][A-Za-z0-9._-]*$/

const asStrings = (v: unknown): string[] => (Array.isArray(v) ? v.map(String) : [])

/** The projects that the draft deletes (an unset of the project's whole entry). */
export function deletedProjects(draft: Draft): Set<string> {
  const out = new Set<string>()
  for (const op of Object.values(draft)) {
    if (op.unset && op.path.length === 2 && op.path[0] === 'projects') out.add(op.path[1] as string)
  }
  return out
}

/**
 * Each project's repos as the draft leaves them, by project name: the file's projects
 * and the ones the draft adds, less the ones it deletes.
 */
export function projectRepos(doc: ConfigDoc, draft: Draft, added: string[] = []): Map<string, string[]> {
  const names = new Set([...Object.keys(doc.projects), ...added])
  for (const op of Object.values(draft)) {
    if (op.path[0] === 'projects' && op.path.length >= 2) names.add(op.path[1] as string)
  }
  for (const n of deletedProjects(draft)) names.delete(n)
  const out = new Map<string, string[]>()
  for (const n of [...names].sort()) {
    const op = draft[keyOf(['projects', n, 'repos'])]
    const repos = op ? (op.unset ? [] : asStrings(op.value)) : asStrings(asMap(doc.projects[n]?.raw).repos)
    out.set(n, repos)
  }
  return out
}

/** The project a repo is in, if any. */
export function ownerOf(map: Map<string, string[]>, repo: string): string | undefined {
  for (const [name, repos] of map) if (repos.includes(repo)) return name
  return undefined
}

/**
 * Put a repo in a project, or take it out. Putting a repo in a project that another holds
 * it in moves it: both lists change in the one draft.
 */
export function moveRepo(doc: ConfigDoc, draft: Draft, added: string[], project: string, repo: string, member: boolean): Draft {
  const map = projectRepos(doc, draft, added)
  const here = (map.get(project) ?? []).filter((r) => r !== repo)
  let d = draft
  if (member) {
    const owner = ownerOf(map, repo)
    if (owner && owner !== project) {
      d = setValue(d, ['projects', owner, 'repos'], (map.get(owner) ?? []).filter((r) => r !== repo))
    }
    here.push(repo)
  }
  return setValue(d, ['projects', project, 'repos'], here)
}

/** Delete a project: what the draft says about it is dropped and, if the file has it, it is unset. */
export function removeProject(doc: ConfigDoc, draft: Draft, name: string): Draft {
  const d: Draft = {}
  for (const [k, op] of Object.entries(draft)) {
    if (!(op.path[0] === 'projects' && op.path[1] === name)) d[k] = op
  }
  return name in doc.projects ? unsetValue(d, ['projects', name]) : d
}
