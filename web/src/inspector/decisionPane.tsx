import { DecisionCard } from '../components/DecisionCard'
import { advanceFrom } from '../chat/strip'
import { waitingKey } from '../model/waitingStep'
import { href } from '../router'
import { useSnapshot } from '../state/context'
import { useShellMode } from '../shell/useMedia'

/**
 * The full view of one decision: question, why it is the person's, context, options, the plan
 * inline, their own answer. In the Conversation it is the main column (the thread moves to the
 * side); elsewhere it is the inspector's body. Cmd+Enter sends, then the next waiting item opens.
 */
export function DecisionPane({ id }: { id: number }) {
  const snap = useSnapshot()
  const mobile = useShellMode() === 'sheet'
  const d = snap.decisions.find((x) => x.id === id)
  if (!d) {
    return (
      <p className="inspector-body" role="status">
        Decision {id} is not waiting on you any more. It was answered or closed. <a href={href.decisions(id)}>Open it on the Decisions page</a>.
      </p>
    )
  }
  const key = waitingKey({ kind: 'decision', action: 'answer', decision_id: id, title: '' })
  const items = snap.waiting?.items ?? []
  return (
    <div className="inspector-body">
      <DecisionCard d={d} variant="pane" focused mobile={mobile} hasNext={items.some((it) => waitingKey(it) !== key)} onSent={() => advanceFrom(items, key)} />
    </div>
  )
}
