import { useEffect, useMemo, useState } from 'react'
import { api } from '../api/client'
import type { Decision, RunView } from '../api/types'
import { Conversation } from '../conversation/Conversation'
import { laneSource } from '../conversation/api'
import { useNow } from '../hooks'
import { LogTab, RunChanges } from '../pages/Log'
import { storedArtifacts, intentTag, type StoredArtifact } from '../artifacts/api'
import { withOpen } from '../router'

export const RUN_TABS = ['conversation', 'log', 'changes', 'asked', 'produced'] as const
export type RunTab = (typeof RUN_TABS)[number]

const TAB_NAMES: Record<RunTab, string> = { conversation: 'Conversation', log: 'Log', changes: 'Changes', asked: 'Asked', produced: 'Produced' }

/** A tab name from an `open=run:N/tab` address; anything unknown is the conversation. */
export function runTab(tab: string | undefined): RunTab {
  return (RUN_TABS as readonly string[]).includes(tab ?? '') ? (tab as RunTab) : 'conversation'
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

/** The run's four views, shared by the inspector and the run page. */
export function RunPane({ run, tab, onTab, onDone }: { run: RunView; tab: RunTab; onTab: (t: RunTab) => void; onDone?: () => void }) {
  const now = useNow(5000)
  const source = useMemo(() => laneSource(run.lane_id), [run.lane_id])
  return (
    <div className="run-pane">
      <div className="lane-tabs" role="tablist" aria-label="Run views">
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
        {/* The thread and the log stay mounted while hidden, so switching tabs does not read
            the log again or lose its search, follow and expanded calls. */}
        <div hidden={tab !== 'conversation'}>
          <Conversation source={source} run={run.id} now={now} />
        </div>
        <div hidden={tab !== 'log'}>
          <LogTab id={run.id} state={run.state} active={tab === 'log'} onDone={onDone} />
        </div>
        {tab === 'changes' && <RunChanges run={run} now={now} />}
        {tab === 'asked' && <Asked runId={run.id} />}
        {tab === 'produced' && <Produced runId={run.id} />}
      </div>
    </div>
  )
}

/** The inspector's body for `open=run:N[/tab]`: a short header, then the run's views. */
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
      <p className="faint run-inspector-sub">
        {run.agent} · {run.state}
      </p>
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

function Asked({ runId }: { runId: number }) {
  const [list, setList] = useState<Decision[] | null>(null)
  const [error, setError] = useState(false)
  useEffect(() => {
    let current = true
    const read = () =>
      api.runEvents(runId).then(
        (e) => {
          if (!current) return
          setList(Array.isArray(e.decisions) ? e.decisions : [])
          setError(false)
        },
        () => current && setError(true),
      )
    void read()
    // An answer given elsewhere shows here without reopening the tab.
    const t = window.setInterval(() => void read(), 5000)
    return () => {
      current = false
      window.clearInterval(t)
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

/** What the run declared or first produced: its artifacts, each opening the viewer. */
function Produced({ runId }: { runId: number }) {
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
