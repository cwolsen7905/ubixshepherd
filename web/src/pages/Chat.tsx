import { useState } from 'react'
import { useSnapshot } from '../state/context'
import { DeskThread } from '../state/desk'
import { Markdown } from '../chat/md'
import { ChatThread } from '../chat/thread'
import '../styles/conversation.css'
import '../styles/chat.css'

/** The URL as an anchor target when it is http or https; null for anything else. */
export { safeHref } from '../chat/md'

/** The first words of a question that has no summary line, for a one-line place. */
export { clipHead } from '../chat/md'

/** A small markdown subset: paragraphs, headings, fenced code, bullet and numbered lists. */
export { blocks } from '../chat/md'

/** Renders the Markdown component for test imports. */
export { Markdown }

/** QuestionActions for ChatQuestions test imports. */
export { QuestionActions } from '../chat/questions'

/** The chat page. `make` builds the thread for a workspace; tests give it fakes. */
export function ChatPage({ make = (id: number) => new DeskThread(id) }: { make?: (workspaceId: number) => DeskThread }) {
  const snap = useSnapshot()
  const workspaces = snap.status?.workspaces ?? []
  const [pick, setPick] = useState(0)
  // The daemon takes a workspace_id only when it has more than one.
  const workspaceId = workspaces.length > 1 ? (workspaces.find((w) => w.id === pick) ?? workspaces[0])?.id ?? 0 : 0
  if (!snap.status && !snap.error) return <p className="empty">Connecting to the daemon…</p>
  return (
    <ChatThread key={workspaceId} thread={make} workspaceId={workspaceId}>
      {workspaces.length > 1 && (
        <label className="chat-ws">
          <span className="faint">Workspace</span>
          <select value={workspaceId} onChange={(e) => setPick(Number(e.target.value))}>
            {workspaces.map((w) => (
              <option key={w.id} value={w.id}>
                {w.name}
              </option>
            ))}
          </select>
        </label>
      )}
    </ChatThread>
  )
}
