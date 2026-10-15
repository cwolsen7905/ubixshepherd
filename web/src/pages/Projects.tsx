import { BudgetMeter } from '../components/BudgetMeter'
import { go, href } from '../router'
import { useProjectList } from '../state/projects'
import { UNASSIGNED } from '../model/projects'
import { NEW_PROJECT } from '../model/settings'
import type { ProjectSummary } from '../api/types'
import { EmptyState, Skeleton } from '../ui'
import '../styles/projects.css'
import '../styles/pages/project.css'

export const projectHref = (p: Pick<ProjectSummary, 'name' | 'unassigned'>) => (p.unassigned ? href.unassigned() : href.project(p.name))

/** Every project as a card: what runs, what waits, and what it has spent against its budget. */
export function ProjectsPage() {
  const { data, error, loading } = useProjectList()
  const list = data?.projects ?? []
  return (
    <div className="page projects-page">
      <header className="page-head">
        <h1>Projects</h1>
        <p className="page-sub">Repos grouped as the configuration groups them. Repos in no project are listed as {UNASSIGNED}.</p>
        <p className="page-actions">
          <a
            className="ui-btn ui-btn-primary"
            href={href.settings(NEW_PROJECT)}
            onClick={(e) => {
              e.preventDefault()
              go(href.settings(NEW_PROJECT))
            }}
          >
            New project
          </a>
        </p>
      </header>
      {error && <p className="tone-bad">Could not load projects: {error}</p>}
      {loading && !data && !error && <Skeleton lines={3} />}
      {data && list.length === 0 && (
        <EmptyState title="No projects yet">
          No projects are configured and no repo is known. Add a <code>projects:</code> section in Settings or in the
          configuration file.
        </EmptyState>
      )}
      <ul className="project-cards">
        {list.map((p) => (
          <li key={p.unassigned ? '\u0000' : p.name} className="project-card">
            <h2>
              <a
                href={projectHref(p)}
                onClick={(e) => {
                  e.preventDefault()
                  go(projectHref(p))
                }}
              >
                {p.unassigned ? UNASSIGNED : p.name}
              </a>
            </h2>
            <p className="project-repos faint">{p.repos.length === 0 ? 'no repos' : p.repos.join(', ')}</p>
            <dl className="project-counts">
              <div>
                <dt>open</dt>
                <dd>{p.lanes}</dd>
              </div>
              <div>
                <dt>running</dt>
                <dd>{p.running}</dd>
              </div>
              <div className={p.waiting > 0 ? 'tone-warn' : undefined}>
                <dt>waiting on you</dt>
                <dd>{p.waiting}</dd>
              </div>
            </dl>
            <p className="project-spend">
              <BudgetMeter budget={p.budget} today={p.spend_today} />
            </p>
          </li>
        ))}
      </ul>
    </div>
  )
}
