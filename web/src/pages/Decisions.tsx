import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { DecisionView } from '../api/types'
import { DecisionCard, splitQuestion } from '../components/DecisionCard'
import { inline } from '../conversation/Markdown'
import { Glyph } from '../components/Glyph'
import { since } from '../format'
import { useNow } from '../hooks'
import { useSnapshot } from '../state/context'

export function DecisionsPage({ focus }: { focus?: number }) {
  const snap = useSnapshot()
  const now = useNow()
  const [showAnswered, setShowAnswered] = useState(false)
  const [answered, setAnswered] = useState<DecisionView[] | null>(null)
  const lanes = new Map(snap.lanes.map((l) => [`${l.repo}\n${l.name}`, l.id]))

  useEffect(() => {
    if (!showAnswered) return
    let live = true
    api.decisions('answered').then(
      (ds) => live && setAnswered(ds.slice(-30).reverse()),
      () => live && setAnswered([]),
    )
    return () => {
      live = false
    }
  }, [showAnswered, snap.decisions.length])

  const open = snap.decisions
  const first = focus ?? open[0]?.id

  return (
    <div className="page decisions">
      <header className="page-head">
        <h1>Decisions</h1>
        <p className="page-sub">
          {open.length === 0
            ? 'No agent is waiting on you.'
            : `${open.length} waiting on you, oldest first. Only you answer them: pick an option or write your own, then send.`}
        </p>
      </header>

      {open.map((d) => (
        <DecisionCard key={d.id} d={d} laneId={lanes.get(`${d.repo}\n${d.lane}`)} focused={d.id === first} />
      ))}

      {snap.requests.length > 0 && (
        <section className="requests" aria-labelledby="requests-h">
          <h2 id="requests-h" className="section-h">Requests no rule could route</h2>
          <p className="page-sub requests-sub">
            An agent asked another lane for something and Shepherd could not tell which. Route one from the terminal
            with <code>shepherd request route &lt;id&gt; --lane &lt;lane&gt;</code>, or ask the front desk.
          </p>
          <ol className="answered">
            {snap.requests.map((q) => (
              <li key={q.id}>
                <Glyph glyph="!" tone="warn" label="request needs routing" />
                <span>
                  <span className="faint">
                    request {q.id} · {q.kind} from {q.from_agent} in {q.from_lane} ({q.repo}) · waiting {since(q.created, now)}
                  </span>
                  <br />
                  <span className="request-msg">{q.message}</span>
                  {q.note && (
                    <>
                      <br />
                      <span className="faint">{q.note}</span>
                    </>
                  )}
                </span>
              </li>
            ))}
          </ol>
        </section>
      )}

      <div className="page-foot">
        <button type="button" className="link-button" onClick={() => setShowAnswered((s) => !s)} aria-expanded={showAnswered}>
          {showAnswered ? 'Hide answered decisions' : 'Show answered decisions'}
        </button>
      </div>
      {showAnswered && (
        <ol className="answered">
          {answered === null && <li className="faint">Loading…</li>}
          {answered?.length === 0 && <li className="faint">None answered yet.</li>}
          {answered?.map((d) => (
            <li key={d.id}>
              <Glyph glyph="↳" tone="muted" label="answered" />
              <span>
                <span className="faint">decision {d.id} · {d.lane} · {d.agent}</span>
                <br />
                {inline(splitQuestion(d.question).head || d.question)}
                <br />
                <strong>{d.answer}</strong>
              </span>
            </li>
          ))}
        </ol>
      )}
    </div>
  )
}
