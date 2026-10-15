import { DecisionCard } from '../components/DecisionCard'
import { advanceFrom } from '../chat/strip'
import { waitingKey } from '../model/waitingStep'
import { href } from '../router'
import { useSnapshot } from '../state/context'

/**
 * The full view of one decision in the inspector: question, body, why, options and the
 * person's own answer. Cmd+Enter sends, then the next waiting item opens.
 */
export function DecisionPane({ id }: { id: number }) {
  const snap = useSnapshot()
  const d = snap.decisions.find((x) => x.id === id)
  if (!d) {
    return (
      <p className="inspector-body" role="status">
        Decision {id} is not waiting on you any more. It was answered or closed. <a href={href.decisions(id)}>Open it on the Decisions page</a>.
      </p>
    )
  }
  const key = waitingKey({ kind: 'decision', action: 'answer', decision_id: id, title: '' })
  return (
    <div className="inspector-body">
      <DecisionCard d={d} variant="pane" focused onSent={() => advanceFrom(snap.waiting?.items ?? [], key)} />
    </div>
  )
}
