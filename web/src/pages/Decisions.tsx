import { useEffect, useRef, useState } from 'react'
import { api } from '../api/client'
import type { Decision, DecisionView } from '../api/types'
import { Glyph } from '../components/Glyph'
import { since } from '../format'
import { useKeys, useNow } from '../hooks'
import { href } from '../router'
import { useLive, useSnapshot } from '../state/context'

export function DecisionsPage({ focus }: { focus?: number }) {
  const snap = useSnapshot()
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
                {d.question}
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

type Sent = { state: 'idle' } | { state: 'sending' } | { state: 'done'; d: Decision } | { state: 'error'; msg: string }

function DecisionCard({ d, laneId, focused }: { d: DecisionView; laneId?: number; focused: boolean }) {
  const live = useLive()
  const now = useNow()
  const options = d.options ?? []
  const [choice, setChoice] = useState<string>('')
  const [own, setOwn] = useState('')
  const [sent, setSent] = useState<Sent>({ state: 'idle' })
  const ref = useRef<HTMLElement>(null)

  useEffect(() => {
    if (focused) ref.current?.scrollIntoView({ block: 'nearest' })
  }, [focused])

  // Number keys pick an option of the focused decision; they never send it.
  useKeys(
    Object.fromEntries(
      options.slice(0, 9).map((o, i) => [String(i + 1), () => focused && (setChoice(o), setOwn(''))]),
    ),
    [focused, options.join('\n')],
  )

  const words = own.trim() || choice
  const send = async () => {
    if (!words) return
    setSent({ state: 'sending' })
    try {
      setSent({ state: 'done', d: await live.answer(d.id, words) })
    } catch (e) {
      setSent({ state: 'error', msg: e instanceof Error ? e.message : String(e) })
    }
  }
  const id = `decision-${d.id}`

  return (
    <article ref={ref} className={`decision${focused ? ' is-focused' : ''}`} aria-labelledby={`${id}-q`}>
      <Glyph glyph="?" tone="warn" label="decision waiting" />
      <div className="decision-body">
        <p className="decision-meta">
          <span>decision {d.id}</span>
          {laneId !== undefined ? <a href={href.lane(laneId)}>{d.lane}</a> : <span>{d.lane}</span>}
          <span>{d.agent}</span>
          <a href={href.log(d.run_id)}>run {d.run_id}</a>
          <span className="faint">{d.repo}</span>
          <span className="faint">waiting {since(d.created, now)}</span>
        </p>
        <h2 id={`${id}-q`} className="decision-q">
          {d.question}
        </h2>
        {d.why && <p className="decision-why">{d.why}</p>}

        {sent.state === 'done' ? (
          <p className="decision-done" role="status">
            <Glyph glyph="↳" tone="ok" label="answered" /> Answered “{sent.d.answer ?? words}”.
            {sent.d.answer_run ? (
              <>
                {' '}The agent carries on as <a href={href.log(sent.d.answer_run)}>run {sent.d.answer_run}</a>.
              </>
            ) : (
              ' The agent gets it when its run ends.'
            )}
          </p>
        ) : (
          <fieldset className="decision-answer" disabled={sent.state === 'sending'}>
            <legend className="visually-hidden">Your answer to decision {d.id}</legend>
            {options.length > 0 && (
              <ol className="options">
                {options.map((o, i) => {
                  const rec = d.recommendation && o.trim() === d.recommendation.trim()
                  return (
                    <li key={o}>
                      <button
                        type="button"
                        className="option"
                        aria-pressed={choice === o && !own.trim()}
                        onClick={() => {
                          setChoice(o)
                          setOwn('')
                        }}
                      >
                        {i < 9 && <kbd aria-hidden="true">{i + 1}</kbd>}
                        <span className="option-text">{o}</span>
                        {rec && <span className="option-rec">recommended</span>}
                      </button>
                    </li>
                  )
                })}
              </ol>
            )}
            {d.recommendation && !options.some((o) => o.trim() === d.recommendation?.trim()) && (
              <p className="decision-rec">
                <span className="faint">Recommended:</span> {d.recommendation}
              </p>
            )}
            <label className="own">
              <span className="visually-hidden">Or write your own answer</span>
              <textarea
                rows={own ? 3 : 1}
                placeholder={options.length ? 'Or write your own answer' : 'Write your answer'}
                value={own}
                onChange={(e) => setOwn(e.target.value)}
              />
            </label>
            <div className="send">
              <button type="button" className="button button-primary" disabled={!words} onClick={send}>
                {sent.state === 'sending' ? 'Sending…' : words ? `Send “${clip(words, 48)}”` : 'Send answer'}
              </button>
              {sent.state === 'error' && (
                <span className="tone-bad" role="alert">
                  Not sent: {sent.msg}
                </span>
              )}
            </div>
          </fieldset>
        )}
      </div>
    </article>
  )
}

function clip(s: string, n: number) {
  const one = s.split(/\s+/).join(' ')
  return one.length > n ? one.slice(0, n - 1) + '…' : one
}
