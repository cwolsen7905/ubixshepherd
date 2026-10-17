import { useId, useState, type ReactNode } from 'react'
import { Button } from './Button'
import { cx } from './cx'
import { OFFLINE_SEND } from './ContextComposer'
import { DecisionOptions, type DecisionOptionsProps } from './DecisionOptions'
import { Kbd } from './Kbd'
import { useOffline } from './offline'
import './Decision.css'

export type DecisionViewLayout = 'page' | 'mobile'

export interface DecisionPlan {
  /** The linked file's path, in mono. */
  path: string
  /** What Shepherd does with it ("kept local"). */
  tag?: ReactNode
  tagTone?: 'local' | 'warn' | 'plain'
  /** "run 17 · 4.1 KB". */
  meta?: ReactNode
  /** The file, rendered: an `ArtifactView layout="inline"`. */
  children: ReactNode
}

export interface DecisionViewProps {
  /** `page`: the main column at the shared width. `mobile`: the 390px page, options and buttons at least 44px tall. */
  layout?: DecisionViewLayout
  /** The faint line over the question: "Decision 5, claude on feat/export-csv, asked 8m ago". */
  kind: ReactNode
  question: ReactNode
  /** Why this is the person's call. */
  why?: ReactNode
  /** The question's body: context, bullets, files. */
  context?: ReactNode
  /** A recommendation that is not one of the options. */
  recommendation?: ReactNode
  options: DecisionOptionsProps['options']
  picked?: string
  recommended?: string
  onPick?: (option: string) => void
  /** The linked plan, shown inline under the options. */
  plan?: DecisionPlan
  /** The person's own answer. */
  own: string
  onOwn: (text: string) => void
  /** Send what is picked or written. The person's click; nothing else calls it. */
  onSend: () => void
  canSend: boolean
  sending?: boolean
  /** "Ask the desk first". */
  onAsk?: () => void
  /** After an answer: what was sent and the way on, in place of the answer controls. */
  done?: ReactNode
  error?: string
  /** `]` goes to the next question, so the hint says so. */
  hasNext?: boolean
  /** Names the page for a screen reader. */
  label: string
}

/**
 * One decision as a page (`.sh-decision`): who asked, the question, why it is the person's,
 * the context, the options as rows, the plan inline, a field for their own answer and Send. The
 * main column holds it while the thread moves to the side. Only the person's own click sends.
 */
export function DecisionView({
  layout = 'page', kind, question, why, context, recommendation, options, picked, recommended, onPick, plan, own, onOwn, onSend, canSend, sending, onAsk, done, error, hasNext, label,
}: DecisionViewProps) {
  const mobile = layout === 'mobile'
  const { offline } = useOffline()
  const uid = useId()
  const [planOpen, setPlanOpen] = useState(true)
  return (
    <article className={cx('sh-decision', mobile && 'sh-decision--mobile')} data-component="DecisionView" data-layout={layout} aria-label={label}>
      <div className="sh-decision-kind">{kind}</div>
      <h1 className="sh-decision-q">{question}</h1>
      {why && (
        <div className="sh-aside sh-decision-why">
          <b>Why this is yours.</b> {why}
        </div>
      )}
      {context && <div className="sh-decision-context">{context}</div>}
      {recommendation && (
        <div className="sh-aside sh-decision-rec">
          <b>Recommended.</b> {recommendation}
        </div>
      )}
      {options.length > 0 && (
        <>
          <h2 className="sh-decision-h">Options</h2>
          <DecisionOptions options={options} variant="rows" picked={picked} recommended={recommended} onPick={done ? undefined : onPick} label={`Options for ${label}`} tall={mobile} />
        </>
      )}
      {plan && (
        <section className="sh-decision-plan" aria-label="The plan this decision links to">
          <button type="button" className="sh-decision-plan-head" aria-expanded={planOpen} aria-controls={`${uid}plan`} onClick={() => setPlanOpen((o) => !o)}>
            <span className="sh-decision-chev" aria-hidden="true">{planOpen ? '▾' : '▸'}</span>
            <code>{plan.path}</code>
            {plan.tag && <span className={cx('sh-tag', plan.tagTone === 'local' && 'sh-tag--local', plan.tagTone === 'warn' && 'sh-tag--warn')}>{plan.tag}</span>}
            {plan.meta && <span className="sh-decision-plan-meta">{plan.meta}</span>}
          </button>
          {planOpen && (
            <div id={`${uid}plan`} className="sh-decision-plan-body">
              {plan.children}
            </div>
          )}
        </section>
      )}
      {done ?? (
        <>
          <textarea
            className="sh-input sh-decision-own"
            rows={3}
            value={own}
            placeholder={options.length > 0 ? 'Or write your own answer' : 'Write your answer'}
            aria-label={`Your own answer to ${label}`}
            onChange={(e) => onOwn(e.target.value)}
          />
          <div className="sh-actions sh-decision-actions">
            <Button variant="attention" className={cx(mobile && 'sh-tap')} disabled={!canSend || sending || offline} title={offline ? OFFLINE_SEND : undefined} onClick={onSend}>
              {sending ? 'Sending…' : 'Send answer'}
            </Button>
            {onAsk && (
              <Button variant="ghost" className={cx(mobile && 'sh-tap')} onClick={onAsk}>
                Ask the desk first
              </Button>
            )}
            {!mobile && (
              <span className="sh-actions-hint">
                <Kbd keys="⌘↵" />
                send{hasNext ? ', then next' : ''}
                {hasNext && (
                  <>
                    {' '}
                    <Kbd keys="]" />
                    next question
                  </>
                )}
              </span>
            )}
          </div>
          {error && (
            <p className="sh-decision-error" role="alert">
              Not sent: {error}
            </p>
          )}
        </>
      )}
    </article>
  )
}
