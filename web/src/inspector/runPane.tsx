import { useEffect, useMemo, useState } from 'react'
import { api } from '../api/client'
import type { Decision, RunView } from '../api/types'
import { Conversation } from '../conversation/Conversation'
import { laneSource } from '../conversation/api'
import { useNow } from '../hooks'
import { LogTab, RunChanges } from '../pages/Log'
import { href, withOpen } from '../router'

export const RUN_TABS = ['conversation', 'log', 'changes', 'asked'] as const
export type RunTab = (typeof RUN_TABS)[number]

const TAB_NAMES: Record<RunTab, string> = { conversation: 'Conversation', log: 'Log', changes: 'Changes', asked: 'Asked' }

/** A tab name from an `open=run:N/tab` address; anything unknown is the conversation. */
export function runTab(tab: string | undefined): RunTab {
  return (RUN_TABS as readonly string[]).includes(tab ?? '') ? (tab as RunTab) : 'conversation'
}

/** Reads a run, and again every few seconds while it runs. `reload` reads it now. */
export function useRun(id: number) {
  const [run, setRun] = useState<RunView | null>(null)
  const [tick, setTick] = useState(0)
  const running = run?.state === 'running'
  useEffect(() => {
    let current = true
    api.run(id).then((r) => current && setRun(r), () => {})
    return () => {
      current = false
    }
  }, [id, tick])
  useEffect(() => {
    if (!running) return
    const t = window.setInterval(() => setTick((n) => n + 1), 3000)
    return () => window.clearInterval(t)
  }, [running])
  return { run, reload: () => setTick((n) => n + 1) }
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
        {tab === 'conversation' && <Conversation source={source} run={run.id} now={now} />}
        {tab === 'log' && <LogTab run={run} onDone={onDone} />}
        {tab === 'changes' && <RunChanges run={run} now={now} />}
        {tab === 'asked' && <Asked runId={run.id} />}
      </div>
    </div>
  )
}

/** The inspector's body for `open=run:N[/tab]`: a short header, then the run's views. */
export function RunInspector({ id, tab }: { id: number; tab?: string }) {
  const { run, reload } = useRun(id)
  const [picked, setPicked] = useState<RunTab>(runTab(tab))
  const [seen, setSeen] = useState(tab)
  // The address leads: Back and a pasted link change the tab.
  if (seen !== tab) {
    setSeen(tab)
    setPicked(runTab(tab))
  }
  if (!run) return <p className="inspector-body faint">Reading run {id}…</p>
  const current = picked
  return (
    <div className="inspector-body run-inspector">
      <p className="faint run-inspector-sub">
        <a href={href.lane(run.lane_id)}>{run.lane}</a> · {run.agent} · {run.state} ·{' '}
        <a href={href.log(run.id)}>Open as page</a>
      </p>
      <RunPane
        run={run}
        tab={current}
        onTab={(t) => {
          setPicked(t)
          window.location.hash = withOpen(window.location.hash, { kind: 'run', id, tab: t })
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
    api.runEvents(runId).then(
      (e) => current && setList(Array.isArray(e.decisions) ? e.decisions : []),
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
          <span className="faint"> {d.state === 'open' ? 'waiting on you' : `answered: ${d.answer ?? ''}`}</span>
        </li>
      ))}
    </ol>
  )
}
