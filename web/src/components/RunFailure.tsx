import { useState } from 'react'
import { runFailures } from '../api/client'
import type { Diagnosis, Fix, FixResult, RunView } from '../api/types'
import { href } from '../router'
import '../styles/failure.css'

/** What taking a fix will do, in the words the person confirms. */
export function explain(f: Fix, d: Diagnosis): string {
  const arg = (k: string) => f.args?.[k] ?? ''
  switch (f.op) {
    case 'continue':
      return 'Continues the run’s agent session with a follow-up that carries the reason and the output above, so it can fix what failed.'
    case 'retry':
      return 'Starts a new agent session in this lane with the same task.'
    case 'retry_other': {
      const who = [arg('agent'), arg('model')].filter(Boolean).join(' ')
      return `Starts a new agent session in this lane with the same task${who ? ` on ${who}` : ' on another agent or model'}.`
    }
    case 'widen_scope': {
      const paths = (arg('paths') || (d.files ?? []).join('\n')).split('\n').filter(Boolean)
      return `Adds ${paths.length ? paths.join(', ') : 'the files named above'} to this lane’s scope, so the agent may change ${paths.length === 1 ? 'it' : 'them'} and the push hook stops refusing ${paths.length === 1 ? 'it' : 'them'}. Other lanes’ scopes are not changed.`
    }
    case 'ship':
      return 'Runs the repo’s gate again and, if it passes, pushes the lane.'
    case 'wait_quota':
      return d.reset_at ? `Nothing to do: runs resume when the agent’s quota resets at ${new Date(d.reset_at).toLocaleString()}.` : 'Nothing to do: runs resume when the agent’s quota resets.'
    case 'open_log':
      return 'Opens the run’s log.'
    default:
      return 'This is a change only you make; Shepherd cannot do it from here.'
  }
}

/** The fixes Shepherd carries out here; the rest are the person's own edits. */
const DOABLE = new Set(['continue', 'retry', 'retry_other', 'ship', 'widen_scope'])

/**
 * Why a run failed and what to do about it: the one-line reason, the evidence in a
 * disclosure (fetched on first open when the list left it out), and the fixes as buttons.
 * Anything marked needs_person asks for a confirmation that says what it will do.
 */
export function RunFailure({
  run, compact = false, onFixed,
}: {
  run: Pick<RunView, 'id' | 'diagnosis' | 'state'>
  compact?: boolean
  /** Called after a fix took, so the page can read the lists again. */
  onFixed?: () => void
}) {
  const d = run.diagnosis
  const [evidence, setEvidence] = useState<string | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [confirming, setConfirming] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<FixResult | null>(null)
  const [error, setError] = useState<string | null>(null)
  if (!d) return null

  const shown = evidence ?? d.evidence ?? ''
  const hasEvidence = !!d.evidence || !!d.evidence_omitted
  const open = () => {
    if (evidence !== null || d.evidence || !d.evidence_omitted) return
    runFailures.diagnoses(run.id).then(
      (ds) => setEvidence(ds.find((x) => x.id === d.id)?.evidence ?? ds[0]?.evidence ?? ''),
      (e: unknown) => setLoadError(e instanceof Error ? e.message : String(e)),
    )
  }

  const take = async (f: Fix) => {
    setBusy(true)
    setError(null)
    try {
      const r = await runFailures.fix(run.id, f.id)
      setResult(r)
      setConfirming(null)
      onFixed?.()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const fixes = d.fixes ?? []
  const body = (
    <>
      {(d.command || d.files?.length) && (
        <p className="failure-meta">
          {d.command && (
            <>
              <code>{d.command}</code>
              {d.exit_code !== undefined && ` exited ${d.exit_code}`}
            </>
          )}
          {d.command && d.files?.length ? ' · ' : ''}
          {d.files?.length ? <>files: {d.files.map((f, i) => <span key={f}>{i > 0 && ', '}<code>{f}</code></span>)}</> : null}
        </p>
      )}
      {hasEvidence && (
        <details className="failure-evidence" onToggle={(e) => e.currentTarget.open && open()}>
          <summary>Evidence</summary>
          {loadError ? <p className="tone-bad">Could not load it: {loadError}</p> : <pre>{shown || (evidence === null && d.evidence_omitted ? 'Loading…' : '')}</pre>}
        </details>
      )}
      {fixes.length > 0 && (
        <ul className="failure-fixes" aria-label="Suggested fixes">
          {fixes.map((f) => (
            <li key={f.id}>
              {f.op === 'open_log' ? (
                <a href={href.log(run.id)}>{f.label}</a>
              ) : f.op === 'wait_quota' ? (
                <span className="faint">{explain(f, d)}</span>
              ) : DOABLE.has(f.op) ? (
                <button
                  type="button"
                  className="link-button"
                  disabled={busy}
                  onClick={() => (f.needs_person ? setConfirming(confirming === f.id ? null : f.id) : void take(f))}
                >
                  {f.label}
                  {f.needs_person ? '…' : ''}
                </button>
              ) : (
                <span className="faint" title={explain(f, d)}>Yours to do: {f.label}</span>
              )}
            </li>
          ))}
        </ul>
      )}
      {fixes.filter((f) => f.id === confirming).map((f) => (
        <div key={f.id} className="failure-confirm" role="group" aria-label={`Confirm: ${f.label}`}>
          <p>{explain(f, d)}</p>
          <button type="button" className="link-button" disabled={busy} onClick={() => void take(f)}>
            Confirm: {f.label}
          </button>{' '}
          <button type="button" className="link-button" disabled={busy} onClick={() => setConfirming(null)}>
            Cancel
          </button>
        </div>
      ))}
      {error && <p className="failure-result tone-bad" role="alert">{error}</p>}
      {result && (
        <p className="failure-result" role="status">
          {result.message ?? 'Done.'}
          {result.run && <> <a href={href.log(result.run.id)}>Run {result.run.id}</a></>}
        </p>
      )}
    </>
  )

  if (compact) {
    return (
      <details className="failure failure-compact">
        <summary>{d.summary}</summary>
        {body}
      </details>
    )
  }
  return (
    <section className="failure" aria-label={`Why run ${run.id} ${run.state}`}>
      <p className="failure-reason">{d.summary}</p>
      {body}
    </section>
  )
}
