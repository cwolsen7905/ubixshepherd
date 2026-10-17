import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { RunView } from '../api/types'
import { BackLink } from '../components/BackLink'
import { Changes } from '../components/Changes'
import { EventGlyph } from '../components/Glyph'
import { RunFailure } from '../components/RunFailure'
import { costNote, duration, runCost, since } from '../format'
import { useNow } from '../hooks'
import { endedBadly } from '../model/board'
import { href } from '../router'
import { useLive } from '../state/context'
import { useRun, type RunTab } from '../inspector/runPane'
import { RunLog } from '../ui/RunLog'

const RUN_EVENT: Record<RunView['state'], string> = {
  running: 'run_started',
  succeeded: 'run_passed',
  failed: 'run_failed',
  interrupted: 'run_interrupted',
  stopped: 'run_interrupted',
}

export { RunLog as LogTab }

export function LogPage({ run: id }: { run: number }) {
  const live = useLive()
  const now = useNow(5000)
  const { run, error, reload } = useRun(id)
  const [tab, setTab] = useState<RunTab>('conversation')

  useEffect(() => {
    if (run?.ended) live.markSeen(`run:${run.id}`)
  }, [live, run?.id, run?.ended])

  return (
    <div className="page log">
      <BackLink />
      <header className="page-head log-head">
        <h1>
          {run && <EventGlyph event={RUN_EVENT[run.state]} />}
          Run {id}
        </h1>
        {run && (
          <p className="page-sub lane-sub">
            <a href={href.lane(run.lane_id)}>{run.lane}</a>
            <span>{run.agent}{run.model ? ` (${run.model})` : ''}</span>
            <span>{run.state}</span>
            <span>{duration(run.started, run.ended, now)}</span>
            {runCost(run) && <span title={costNote(run)}>{runCost(run)}</span>}
            {run.parent ? <a href={href.log(run.parent)}>continues run {run.parent}</a> : null}
            {run.dismissed && <span title={run.dismissed}>dismissed from the board {since(run.dismissed, now)} ago</span>}
          </p>
        )}
        {run && endedBadly(run) && (
          <p className="row-actions">
            <button
              type="button"
              className="link-button"
              title={run.dismissed ? undefined : 'Clear this run from the board. Its log and record stay in history.'}
              onClick={() => live.dismiss(run.id, !run.dismissed).then(reload, () => {})}
            >
              {run.dismissed ? 'Undismiss' : 'Dismiss'}
            </button>
          </p>
        )}
      </header>

      {run?.diagnosis && <RunFailure run={run} onFixed={reload} />}

      {error && (
        <p className="tone-bad">
          Could not read the run: {error}.{' '}
          <button type="button" className="link-button" onClick={reload}>
            Retry
          </button>
        </p>
      )}

      {run ? (
        <div className="log-page">
          <div role="tablist" aria-label="Run views" className="lane-tabs">
            {(['conversation', 'log', 'changes', 'asked', 'produced'] as const).map((t) => (
              <button
                key={t}
                type="button"
                role="tab"
                id={`log-tab-${t}`}
                className="lane-tab"
                aria-selected={tab === t}
                aria-controls="log-panel"
                onClick={() => setTab(t)}
              >
                {t.charAt(0).toUpperCase() + t.slice(1)}
              </button>
            ))}
          </div>
          <div id="log-panel" role="tabpanel" aria-labelledby={`log-tab-${tab}`}>
            <div hidden={tab !== 'conversation'}>
              TODO: conversation
            </div>
            <div hidden={tab !== 'log'}>
              <RunLog id={run.id} state={run.state} variant={run.state === 'running' ? 'live' : 'finished'} active={tab === 'log'} onDone={reload} />
            </div>
            {tab === 'changes' && <RunChanges run={run} now={now} />}
            {tab === 'asked' && <p className="faint">This run asked nothing yet.</p>}
            {tab === 'produced' && <p className="faint">No produced artifacts yet.</p>}
          </div>
        </div>
      ) : (
        <RunLog id={id} state={undefined} variant="finished" />
      )}
    </div>
  )
}

/**
 * The run's own changes: the lane's Changes view narrowed to the run's start_sha..end_sha.
 * Read when its tab is shown, since each read runs git; whether the run is the lane's latest
 * (the only one that shows the worktree's uncommitted work) is read then too.
 */
export function RunChanges({ run, now }: { run: RunView; now: number }) {
  const open = true
  const [latest, setLatest] = useState<boolean | null>(null)
  const [editorUrl] = useState<string | null>(null)
  const laneId = run.lane_id
  const runId = run.id
  useEffect(() => {
    if (!open) return
    let current = true
    api.runs(laneId, 1).then(
      (rs) => current && setLatest(Array.isArray(rs) && rs[0]?.id === runId),
      () => current && setLatest(false),
    )
    return () => {
      current = false
    }
  }, [open, laneId, runId])
  return (
    <div className="run-changes">
      {open && latest === null && <p className="faint">Reading…</p>}
      {open && latest !== null && (
        <Changes
          laneId={laneId}
          worktree={run.worktree}
          editorUrl={editorUrl}
          refreshKey={0}
          now={now}
          run={{ id: runId, latest, outside: run.outside }}
        />
      )}
    </div>
  )
}
