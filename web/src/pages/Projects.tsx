import { useEffect, useState } from 'react'
import { config as configApi } from '../api/client'
import type { ConfigDoc, ProjectSummary } from '../api/types'
import { go, href } from '../router'
import { useKeys } from '../hooks'
import { getAt } from '../model/settings'
import { UNASSIGNED } from '../model/projects'
import { NEW_PROJECT } from '../model/settings'
import { useProjectList } from '../state/projects'
import { BudgetMeter } from '../components/BudgetMeter'
import { EmptyState, ErrorState, LoadingRows } from '../ui'
import './Projects.css'

export const projectHref = (p: Pick<ProjectSummary, 'name' | 'unassigned'>) => (p.unassigned ? href.unassigned() : href.project(p.name))

/** A repo's merge rule in words: who merges its lanes' merge requests. */
export function mergeRule(doc: ConfigDoc | null, repo: string): string {
  const rule = getAt(doc?.repos?.[repo]?.effective ?? {}, ['autonomy', 'merge']) ?? getAt(doc?.defaults?.effective ?? {}, ['autonomy', 'merge'])
  return rule === 'agent' ? 'Rule: agent merges' : 'By a person'
}

const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? '' : 's'}`

/** The header summary: projects, distinct repos and open lanes. */
function summary(list: ProjectSummary[]): string {
  const repos = new Set(list.flatMap((p) => p.repos)).size
  return `${plural(list.length, 'project')} · ${plural(repos, 'repo')} · ${list.reduce((n, p) => n + p.lanes, 0)} open lanes`
}

/** Every project: its repos, merge rule per repo, open lanes, spend and needs-you count. */
export function ProjectsPage() {
  const { data, error, loading } = useProjectList()
  const [doc, setDoc] = useState<ConfigDoc | null>(null)
  const [cursor, setCursor] = useState(0)
  useEffect(() => {
    let live = true
    configApi.get().then(
      (d) => live && setDoc(d),
      () => live && setDoc(null),
    )
    return () => {
      live = false
    }
  }, [])
  const list = data?.projects ?? []
  useKeys(
    {
      j: () => setCursor((c) => Math.min(list.length - 1, c + 1)),
      k: () => setCursor((c) => Math.max(0, c - 1)),
      Enter: () => {
        const p = list[cursor]
        if (p) go(projectHref(p))
      },
    },
    [list, cursor],
  )
  return (
    <div className="page sh-projects-page">
      <header className="page-head">
        <h1>Projects</h1>
        <div className="page-actions">
          {data && list.length > 0 && <span className="sh-project-summary">{summary(list)}</span>}
          <a
            className="sh-btn sh-btn--primary ui-btn ui-btn-primary"
            href={href.settings(NEW_PROJECT)}
            onClick={(e) => {
              e.preventDefault()
              go(href.settings(NEW_PROJECT))
            }}
          >
            New project
          </a>
        </div>
      </header>
      {error && <ErrorState what="Projects" error={error} />}
      {loading && !data && !error && <LoadingRows />}
      {data && list.length === 0 && (
        <EmptyState title="No projects yet">
          No projects are configured and no repo is known. Add a <code>projects:</code> section in Settings or in the
          configuration file.
        </EmptyState>
      )}
      {list.length > 0 && (
        <div className="sh-projects-scroll">
        <table className="sh-projects-table" data-component="ProjectsTable">
          <thead>
            <tr>
              <th>Project</th>
              <th>Repos and merge rule</th>
              <th className="num">Open lanes</th>
              <th>Spend today</th>
              <th>Needs you</th>
            </tr>
          </thead>
          <tbody>
            {list.map((p, i) => (
              <tr key={p.unassigned ? ' ' : p.name} aria-label={p.unassigned ? UNASSIGNED : p.name} className={i === cursor ? 'is-cursor' : undefined}>
                <td>
                  <a
                    href={projectHref(p)}
                    className="sh-project-name"
                    tabIndex={i === cursor ? 0 : -1}
                    onClick={(e) => {
                      e.preventDefault()
                      setCursor(i)
                      go(projectHref(p))
                    }}
                    onFocus={() => setCursor(i)}
                  >
                    {p.unassigned ? UNASSIGNED : p.name}
                  </a>
                </td>
                <td>
                  {p.repos.length === 0 ? (
                    <span className="sh-faint">no repos</span>
                  ) : (
                    <ul className="sh-project-repos" aria-label="Repos">
                      {p.repos.map((r) => (
                        <li key={r}>
                          <code>{r}</code> <span className="sh-project-rule">{mergeRule(doc, r)}</span>
                        </li>
                      ))}
                    </ul>
                  )}
                </td>
                <td className="num">{p.lanes}</td>
                <td className="sh-project-spend">
                  <BudgetMeter budget={p.budget} today={p.spend_today} />
                </td>
                <td>
                  {p.waiting > 0 ? <span className="sh-project-needs">{p.waiting} waiting</span> : <span className="sh-faint">Nothing</span>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        </div>
      )}
      <p className="sh-project-note">Repos grouped as the configuration groups them. Repos in no project are listed as {UNASSIGNED}.</p>
    </div>
  )
}
