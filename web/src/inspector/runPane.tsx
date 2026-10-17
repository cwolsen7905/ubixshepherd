import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { Decision, RunView } from '../api/types'
import { useNow } from '../hooks'
import { href, withOpen } from '../router'
import { RunLog } from '../ui/RunLog'
import { storedArtifacts, intentTag, type StoredArtifact } from '../artifacts/api'
import { RunChanges } from '../components/RunChanges'

export const RUN_TABS = ['log', 'changes', 'artifacts'] as const
export type RunTab = (typeof RUN_TABS)[number]

const TAB_NAMES: Record<RunTab, string> = { log: 'Log', changes: 'Changes', artifacts: 'Artifacts' }

/** A tab name from an `open=run:N/tab` address; anything unknown is the log. */
export function runTab(tab: string | undefined): RunTab {
  return (RUN_TABS as readonly string[]).includes(tab ?? '') ? (tab as RunTab) : 'log'
}

/** Reads a run, and again every few seconds while it runs. `reload` reads it now. */
export function useRun(id: number) {
  const [run, setRun] = useState<RunView | null>(null)
  const [failed, setFailed] = useState<string | null>(null)
  const [tick, setTick] = useState(0)
  const running = run?.state === 'running'
  useEffect(() => {
    let current = true
    api.run(id).then(
      (r) => {
        if (!current) return
        setRun(r)
        setFailed(null)
      },
      (e) => current && setFailed(e instanceof Error ? e.message : String(e)),
    )
    return () => {
      current = false
    }
  }, [id, tick])
  useEffect(() => {
    if (!running) return
    const t = window.setInterval(() => setTick((n) => n + 1), 3000)
    return () => window.clearInterval(t)
  }, [running])
  return { run, error: failed, reload: () => setTick((n) => n + 1) }
}

function glyphFor(state: RunView['state']) {
  if (state === 'running') return '▸'
  if (state === 'succeeded') return '✓'
  if (state === 'failed' || state === 'interrupted' || state === 'stopped') return '✗'
  return '●'
}

function glyphTone(state: RunView['state']): 'ok' | 'bad' | 'live' | 'idle' {
  if (state === 'succeeded') return 'ok'
  if (state === 'running') return 'live'
  if (state === 'failed' || state === 'interrupted' || state === 'stopped') return 'bad'
  return 'idle'
}

function titleFor(state: RunView['state'], id: number) {
  if (state === 'succeeded') return `Run ${id} succeeded`
  if (state === 'failed') return `Run ${id} failed`
  if (state === 'running') return `Run ${id} running`
  if (state === 'interrupted' || state === 'stopped') return `Run ${id} interrupted`
  return `Run ${id}`
}

function shortModel(model: string | undefined): string {
  if (!model) return ''
  const i = model.lastIndexOf('/')
  return i >= 0 ? model.slice(i + 1) : model
}

function RunArtifacts({ runId }: { runId: number }) {
  const [list, setList] = useState<StoredArtifact[] | null>(null)
  const [error, setError] = useState(false)
  useEffect(() => {
    let current = true
    storedArtifacts.list({ run_id: runId }).then(
      (a) => {
        if (!current) return
        setList(a)
        setError(false)
      },
      () => current && setError(true),
    )
    return () => {
      current = false
    }
  }, [runId])
  if (error) return <p className="tone-bad">Could not read what this run produced.</p>
  if (list === null) return <p className="faint">Reading…</p>
  if (list.length === 0) return <p className="faint">This run produced no artifacts.</p>
  return (
    <ul className="artifact-produced">
      {list.map((a) => (
        <li key={a.id}>
          <a href={withOpen(window.location.hash, { kind: 'artifact', id: a.id })}>{a.path}</a>{' '}
          <span className="faint">{intentTag(a)}</span>
        </li>
      ))}
    </ul>
  )
}

/** The run's three views: Log, Changes, Artifacts. */
export function RunPane({ run, tab, onTab, onDone }: { run: RunView; tab: RunTab; onTab: (t: RunTab) => void; onDone?: () => void }) {
  const now = useNow(5000)
  return (
    <div className="run-pane" data-component="RunPane">
      <p className="run-title">
        <span className={`sh-glyph sh-glyph--${glyphTone(run.state)}`} aria-hidden="true">{glyphFor(run.state)}</span>
        <span>{titleFor(run.state, run.id)}</span>
      </p>
      <p className="run-meta faint">
        <a href={href.lane(run.lane_id)}>{run.repo} / {run.lane}</a>
        <span>{run.agent}{run.model ? ` · ${shortModel(run.model)}` : ''}</span>
        <span>{run.state}</span>
      </p>
      <div role="tablist" aria-label="Run views" className="lane-tabs">
        {RUN_TABS.map((t) => (
          <button
            key={t}
            type="button"
            role="tab"
            id={`run-tab-${t}`}
            className="lane-tab"
            aria-selected={tab === t}
            aria-controls="run-panel"
            onClick={() => onTab(t)}
          >
            {TAB_NAMES[t]}
          </button>
        ))}
      </div>
      <div id="run-panel" role="tabpanel" aria-labelledby={`run-tab-${tab}`}>
        <div hidden={tab !== 'log'}>
          <RunLog id={run.id} state={run.state} active={tab === 'log'} variant={run.state === 'running' ? 'live' : 'finished'} onDone={onDone} />
        </div>
        {tab === 'changes' && <RunChanges run={run} now={now} />}
        {tab === 'artifacts' && <RunArtifacts runId={run.id} />}
      </div>
    </div>
  )
}

/** The inspector's body for `open=run:N[/tab]` and the run page. */
export function RunInspector({ id, tab, full }: { id: number; tab?: string; full?: boolean }) {
  const { run, error, reload } = useRun(id)
  const [picked, setPicked] = useState<RunTab>(runTab(tab))
  const [seen, setSeen] = useState(tab)
  // The address leads: Back and a pasted link change the tab.
  if (seen !== tab) {
    setSeen(tab)
    setPicked(runTab(tab))
  }
  if (!run && error) {
    return (
      <div className="inspector-body">
        <p className="tone-bad">Could not read run {id}: {error}</p>
        <button type="button" className="link-button" onClick={reload}>
          Retry
        </button>
      </div>
    )
  }
  if (!run) return <p className="inspector-body faint">Reading run {id}…</p>
  const current = picked
  return (
    <div className="inspector-body run-inspector">
      <RunPane
        run={run}
        tab={current}
        onTab={(t) => {
          setPicked(t)
          window.location.hash = withOpen(window.location.hash, { kind: 'run', id, tab: t, ...(full ? { full: true } : {}) })
        }}
        onDone={reload}
      />
    </div>
  )
}

/** Kept for compatibility: old code re-exports Asked helpers here. */
export function Asked({ runId }: { runId: number }) {
  const [list, setList] = useState<Decision[] | null>(null)
  const [error, setError] = useState(false)
  useEffect(() => {
    let current = true
    api.runEvents(runId).then(
      (e) => {
        if (!current) return
        setList(Array.isArray(e.decisions) ? e.decisions : [])
        setError(false)
      },
      () => current && setError(true),
    )
    return () => {
      current = false
    }
  }, [runId])
  if (error) return <p className="tone-bad">Could not read what this run asked.</p>
  if (list === null) return <p className="faint">Reading…</p>
  if (list.length === 0) return <p className="faint">This run asked nothing.</p>
  return (
    <ol className="run-asked">
      {list.map((d) => (
        <li key={d.id}>
          <a href={withOpen(window.location.hash, { kind: 'decision', id: d.id })}>{d.question.split('\n')[0]}</a>
          <span className="faint"> {d.state === 'open' ? 'waiting on you' : d.answer ? `answered: ${d.answer}` : 'answered'}</span>
        </li>
      ))}
    </ol>
  )
}
