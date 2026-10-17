import { useEffect, useMemo, useState } from 'react'
import { config as configApi } from '../api/client'
import type { ConfigDoc } from '../api/types'
import { Chip, EmptyState, GroupHeader } from '../ui'
import './Rules.css'

export type RuleLayer = 'built-in' | 'workspace' | 'project' | 'repo'

export interface Rule {
  id: string
  layer: RuleLayer
  /** Where the rule comes from: "Shepherd", "config defaults", "project shop", "repo acme-api". */
  source: string
  /** What the rule is about: "Merge", "Push". */
  name: string
  /** Who acts under it: "agent", "shepherd" or "person". */
  holder: string
  /** What it allows, in words. */
  allows: string
}

export const LAYERS: { id: RuleLayer; label: string; hint: string }[] = [
  { id: 'built-in', label: 'Built in', hint: 'What Shepherd does without any config' },
  { id: 'workspace', label: 'Workspace', hint: 'The defaults section of the config' },
  { id: 'project', label: 'Project', hint: 'A project section that sets autonomy' },
  { id: 'repo', label: 'Repo', hint: 'Each repo, with what it sets itself' },
]

const AUTONOMY = ['push', 'merge', 'tag', 'deploy'] as const
type Verb = (typeof AUTONOMY)[number]

const NAMES: Record<Verb, string> = { push: 'Push', merge: 'Merge', tag: 'Tag', deploy: 'Deploy' }

/** What a choice of one autonomy field lets happen. */
export function allows(verb: Verb, value: string): { holder: string; text: string } {
  const v = value || 'human'
  if (verb === 'push') {
    if (v === 'agent') return { holder: 'agent', text: "An agent may push its own lane's branch during its run." }
    if (v === 'shepherd') return { holder: 'shepherd', text: "Shepherd pushes a lane's branch and opens its merge request once the repo gate passes." }
    return { holder: 'person', text: 'Only the person pushes a lane.' }
  }
  if (verb === 'merge') {
    if (v === 'agent') return { holder: 'agent', text: "An agent may arm merge-when-pipeline-succeeds on its own lane's merge request. The forge's approvals, pipelines and threads still gate it." }
    return { holder: 'person', text: 'Only the person merges, through the approval flow.' }
  }
  if (v === 'agent') return { holder: 'agent', text: `An agent may ${verb} without asking.` }
  return { holder: 'person', text: `Only the person may ${verb}.` }
}

function autonomyOf(o: unknown): Partial<Record<Verb, string>> {
  const a = (o as { autonomy?: Record<string, unknown> } | undefined)?.autonomy
  const out: Partial<Record<Verb, string>> = {}
  for (const v of AUTONOMY) if (typeof a?.[v] === 'string') out[v] = a[v] as string
  return out
}

/** The rules the config holds today, layer by layer. Crook's own rule files are not read yet. */
export function rulesFrom(doc: ConfigDoc): Rule[] {
  const out: Rule[] = [
    { id: 'built-in:own-work', layer: 'built-in', source: 'Shepherd', name: 'Approval', holder: 'person', allows: 'An agent never approves its own work.' },
  ]
  for (const v of AUTONOMY) {
    const a = allows(v, 'human')
    out.push({ id: `built-in:${v}`, layer: 'built-in', source: 'Shepherd', name: NAMES[v], holder: a.holder, allows: a.text })
  }
  const base = autonomyOf(doc.defaults?.effective)
  for (const v of AUTONOMY) {
    const a = allows(v, base[v] ?? '')
    out.push({ id: `workspace:${v}`, layer: 'workspace', source: 'config defaults', name: NAMES[v], holder: a.holder, allows: a.text })
  }
  for (const name of Object.keys(doc.projects ?? {}).sort()) {
    const set = autonomyOf(doc.projects[name]?.raw)
    for (const v of AUTONOMY) {
      if (set[v] === undefined) continue
      const a = allows(v, set[v])
      out.push({ id: `project:${name}:${v}`, layer: 'project', source: `project ${name}`, name: NAMES[v], holder: a.holder, allows: a.text })
    }
  }
  for (const name of Object.keys(doc.repos ?? {}).sort()) {
    const p = doc.repos[name]
    const eff = autonomyOf(p?.effective)
    for (const v of AUTONOMY) {
      if (!(p?.overridden ?? []).includes(`autonomy.${v}`)) continue
      const a = allows(v, eff[v] ?? '')
      out.push({ id: `repo:${name}:${v}`, layer: 'repo', source: `repo ${name}`, name: NAMES[v], holder: a.holder, allows: a.text })
    }
  }
  return out
}

export type Verdict = 'refuse' | 'hold' | 'ask' | 'proceed'

/** What the rule does to an agent: refuse what no one may do, hold what waits for the person, proceed where an agent or Shepherd may act. */
export function verdictOf(r: Rule): Verdict {
  if (r.id === 'built-in:own-work') return 'refuse'
  return r.holder === 'person' ? 'hold' : 'proceed'
}

/** The layers that decide what an agent may do unasked: the built-in rules, then the config's workspace, project and repo sections. */
export function RulesPage() {
  const [state, setState] = useState<{ doc?: ConfigDoc; error?: string } | null>(null)
  useEffect(() => {
    let live = true
    configApi.get().then(
      (doc) => live && setState({ doc }),
      (e: unknown) => live && setState({ error: e instanceof Error ? e.message : String(e) }),
    )
    return () => {
      live = false
    }
  }, [])
  const [view, setView] = useState<'rules' | 'shadow'>('rules')
  const rules = useMemo(() => (state?.doc ? rulesFrom(state.doc) : []), [state])

  return (
    <div className="page page-wide rules-page" data-component="Rules">
      <header className="page-head">
        <h1>Rules</h1>
        <div className="page-chips">
          <Chip kind="filter" selected={view === 'rules'} onClick={() => setView('rules')}>
            Rules <span className="rules-n">{rules.length}</span>
          </Chip>
          <Chip kind="filter" selected={view === 'shadow'} onClick={() => setView('shadow')}>Shadow log</Chip>
        </div>
        <div className="page-actions rules-mode">
          <span className="faint">Mode</span>
          <div className="sh-segmented" role="group" aria-label="Mode">
            <button type="button" aria-pressed="true" disabled>Enforce</button>
            <button type="button" aria-pressed="false" disabled>Shadow</button>
          </div>
        </div>
      </header>
      <p className="rules-note faint">Mode is fixed until Crook's rule engine lands: the rules below are enforced as the config sets them.</p>
      {view === 'shadow' ? (
        <section aria-label="Shadow log">
          <GroupHeader label="Shadow log" hint="What a rule would have decided" />
          <EmptyState title="No shadow verdicts yet.">They appear once Crook logs them.</EmptyState>
        </section>
      ) : (
        <>
        <p className="rules-order">Later layers win: a repo rule overrides its project, which overrides the workspace, which overrides built-in.</p>
        {state?.error && <p className="tone-bad rules-pad">Could not load the rules: {state.error}</p>}
        {!state && <p className="faint rules-pad">Loading…</p>}
        <section aria-label="Proposed by Shepherd">
          <GroupHeader label="Proposed by Shepherd" hint="learned from your answers · runs in shadow until you approve" />
          <EmptyState title="No proposed rules.">Shepherd proposes a rule once it learns a pattern in your decisions. That needs Crook's rule files, which are not built yet.</EmptyState>
        </section>
        {state?.doc &&
          LAYERS.map((l) => {
            const mine = rules.filter((r) => r.layer === l.id)
            return (
              <section key={l.id} aria-label={l.label}>
                <GroupHeader label={l.label} hint={l.hint} />
                {mine.length === 0 ? (
                  <p className="faint rules-none">Nothing is set at this layer.</p>
                ) : (
                  <table className="rules-table">
                    <tbody>
                      {mine.map((r) => (
                        <tr key={r.id}>
                          <td className="rules-id"><code>{r.id}</code></td>
                          <td className="rules-what">
                            {r.name}: {r.holder}. <span className="rules-allows">{r.allows}</span>
                          </td>
                          <td className="rules-verdict-col"><span className={`sh-tag rules-verdict--${verdictOf(r)}`}>{verdictOf(r)}</span></td>
                          <td className="rules-source"><code>{r.source}</code></td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                )}
              </section>
            )
          })}
        </>
      )}
    </div>
  )
}
