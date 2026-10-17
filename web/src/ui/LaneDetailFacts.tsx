import type { LaneDiff, LaneView, RunView } from '../api/types'
import { GateBadge } from '../components/GateBadge'
import { MrBadge } from '../components/MrBadge'
import { editorHref } from '../editorLink'
import { clockTime, duration, runCost, shortSha } from '../format'
import type { LaneApprovalState } from './LaneDetailApprove'

/** The daemon gives a pipeline's id but not its URL; on GitLab it sits beside the MR's. */
function pipelineLink(lane: LaneView): string | null {
  if (!lane.mr_url || !lane.pipeline) return null
  const m = /^(.*)\/-\/merge_requests\/\d+$/.exec(lane.mr_url)
  return m ? `${m[1]}/-/pipelines/${lane.pipeline}` : null
}

function origin(l: LaneView): string {
  const o = l.origin
  if (!o.via) return 'unknown'
  const parts = [o.agent, o.run ? `run ${o.run}` : '', o.detail].filter(Boolean)
  return parts.length ? `${o.via} (${parts.join(', ')})` : o.via
}

/** What the lane's changes read says, for the rail: counts, when it was read and Refresh. */
export interface FactsRead {
  read: LaneDiff | null
  reading: boolean
  onRefresh: () => void
}

/**
 * The lane's facts (`.sh-dl`): merge request and its state, pipeline, gate, scope, commits,
 * worktree with "open in editor", who opened it. The page shows them as a right rail beside the
 * Overview and the Changes list, the panel inline. On the Changes tab `changes` swaps the commit
 * list from Shepherd's runs for the commits the read found, and adds the read's own line.
 */
export function LaneFacts({ lane, runs, latest, now, editorUrl, page, approval, changes }: {
  lane: LaneView
  runs: RunView[]
  latest?: RunView
  now: number
  editorUrl: string | null
  page: boolean
  approval?: LaneApprovalState
  changes?: FactsRead
}) {
  const link = editorHref(editorUrl, lane.worktree)
  const view = approval?.view
  // The lane's own record leads; the forge's read fills in what the lane has not caught up with.
  const pipeline = lane.pipeline ?? view?.pipeline
  const pipelineStatus = lane.pipeline ? lane.pipeline_status : view?.pipeline_status
  const pipelineUrl = lane.pipeline ? pipelineLink(lane) : (view?.pipeline_url ?? null)
  const commits = runs.filter((r) => r.commits > 0)
  const read = changes?.read
  const files = read ? read.committed.length + read.uncommitted.length + read.untracked.length + (read.files_more ?? 0) : 0
  const outside = read ? [...read.committed, ...read.uncommitted, ...read.untracked].filter((f) => !f.in_scope).length : 0
  return (
    <dl className="sh-dl">
      {!page && (
        <>
          <dt>Repo</dt>
          <dd>{lane.repo}</dd>
          <dt>Branch</dt>
          <dd>
            <code>{lane.branch}</code>
            {lane.base && <> onto <code>{lane.base}</code></>}
          </dd>
        </>
      )}
      <dt>Merge request</dt>
      <dd>
        {lane.mr || view?.mr ? (
          <>
            {lane.mr ? <MrBadge lane={lane} /> : <a href={view?.mr_url} target="_blank" rel="noreferrer">!{view?.mr}</a>}{' '}
            <span className="faint">{lane.mr ? lane.mr_state : view?.mr_state}</span>
            {view?.approved ? (
              <span className="tone-ok"> approved{view.approved_by?.length ? ` by ${view.approved_by.join(', ')}` : ''}</span>
            ) : approval?.canApprove ? (
              <span className="tone-warn"> waits for your approval</span>
            ) : null}
            {pipeline ? (
              <>
                {' '}
                {pipelineUrl ? <a href={pipelineUrl} target="_blank" rel="noreferrer">pipeline {pipeline}</a> : `pipeline ${pipeline}`}{' '}
                <span className="faint">{pipelineStatus}</span>
              </>
            ) : (
              <span className="faint"> no pipeline seen</span>
            )}
            {view?.require_approval && (
              <span className={view.require_approval === 'success' ? 'tone-ok' : view.require_approval === 'failed' || view.require_approval === 'canceled' ? 'tone-bad' : 'faint'}> require-approval {view.require_approval}</span>
            )}
          </>
        ) : (
          <span className="faint">None yet</span>
        )}
      </dd>
      {lane.gate && (
        <>
          <dt>Gate</dt>
          <dd><GateBadge gate={lane.gate} now={now} /></dd>
        </>
      )}
      <dt>Scope</dt>
      <dd>
        <ul className="scope">
          {lane.scope.map((s) => (
            <li key={s}><code>{s}</code></li>
          ))}
        </ul>
      </dd>
      <dt>Commits</dt>
      <dd>
        {read ? (
          read.commits.length > 0 ? (
            <>
              <ul className="commits">
                {read.commits.map((c) => (
                  <li key={c.sha} title={`${c.subject}, ${c.files} ${c.files === 1 ? 'file' : 'files'}`}>
                    <code>{shortSha(c.sha)}</code> {c.subject}
                  </li>
                ))}
              </ul>
              {read.commits_total > read.commits.length && <span className="faint">The newest {read.commits.length} of {read.commits_total}.</span>}
            </>
          ) : (
            <span className="faint">No commits ahead of the base</span>
          )
        ) : commits.length > 0 ? (
          <ul className="commits">
            {commits.map((r) => (
              <li key={r.id}>
                <code>{shortSha(r.start_sha)}..{shortSha(r.end_sha)}</code> {r.commits} by run {r.id}
              </li>
            ))}
          </ul>
        ) : (
          <span className="faint">None from Shepherd's runs</span>
        )}
      </dd>
      {changes && read && (
        <>
          <dt>Read</dt>
          <dd>
            <span className="faint">
              <code>{shortSha(read.head)}</code> against <code>{read.base_ref || read.base}</code>, {read.commits_total} {read.commits_total === 1 ? 'commit' : 'commits'}, {files} {files === 1 ? 'file' : 'files'}
            </span>
            {outside > 0 && <> <span className="tone-warn">{outside} outside scope</span></>}
            <br />
            <span className="faint">
              Read <time dateTime={read.read_at} title={read.read_at}>{clockTime(read.read_at, now)}</time>
            </span>{' · '}
            <button type="button" className="link-button" disabled={changes.reading} onClick={changes.onRefresh}>
              {changes.reading ? 'Reading…' : 'Refresh'}
            </button>
          </dd>
        </>
      )}
      <dt>Worktree</dt>
      <dd>
        <code className="path">{lane.worktree}</code>
        {link && <> <a href={link} title="Open in editor">open in editor</a></>}
      </dd>
      <dt>Opened by</dt>
      <dd>{origin(lane)}</dd>
      {latest && !page && (
        <>
          <dt>Latest run</dt>
          <dd>
            Run {latest.id} · {latest.agent}
            {' · '}
            {latest.state}
            {latest.ended && <> · {duration(latest.started, latest.ended, now)}</>}
            {runCost(latest) && <> · {runCost(latest)}</>}
          </dd>
        </>
      )}
    </dl>
  )
}
