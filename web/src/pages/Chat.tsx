import { useEffect, useState } from 'react'
import { topics as topicsApi } from '../api/client'
import { useSnapshot } from '../state/context'
import { readTopicFilter, withTopicFilter } from '../model/topic'
import { parse } from '../router'
import { DeskThread } from '../state/desk'
import { Markdown } from '../chat/md'
import { ChatThread } from '../chat/thread'
import { useWorkspace } from '../chat/workspace'

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

/** Where the topic the person last chose to follow is kept, so the chat opens on it again. */
export const TOPIC_KEY = 'shepherd.chat.topic'

/** The remembered topic, or null when none is kept or storage cannot be read. */
export function readChosenTopic(): number | null {
  try {
    const n = Number(window.localStorage.getItem(TOPIC_KEY))
    return Number.isInteger(n) && n > 0 ? n : null
  } catch {
    return null
  }
}

/** Keeps the topic the person chose, or forgets it when they choose All. */
export function writeChosenTopic(id: number | null): void {
  try {
    if (id === null) window.localStorage.removeItem(TOPIC_KEY)
    else window.localStorage.setItem(TOPIC_KEY, String(id))
  } catch {
    // Private mode or a full quota: the choice lasts as long as the address does.
  }
}

/**
 * The address with the remembered topic put back, when the address names none: the chat
 * opens on the topic the person last followed. An address that names a topic wins, so a
 * link to one still works. Replaced, not pushed, so Back does not step through it.
 */
export function restoreChosenTopic(): number | null {
  const hash = window.location.hash
  if (readTopicFilter(hash) !== null) return null
  const id = readChosenTopic()
  if (id === null) return null
  window.history.replaceState(null, '', withTopicFilter(hash || '#/', id))
  return id
}

/**
 * Drops a remembered topic that no longer exists (merged away, or of another workspace): once the
 * topic list has loaded without it, the address and the kept choice go back to All, so the thread
 * never shows every row under a dead topic and the composer files nothing under it. Only a topic
 * that was put back from storage is checked; one a link names is the link's to keep. A failed
 * read changes nothing.
 */
function useDropStaleTopic(restored: number | null) {
  useEffect(() => {
    if (restored === null) return
    let live = true
    topicsApi.list(true).then(
      (l) => {
        if (!live || l.topics.some((t) => t.id === restored)) return
        if (readTopicFilter(window.location.hash) !== restored) return
        writeChosenTopic(null)
        window.location.replace(withTopicFilter(window.location.hash, null))
      },
      () => {},
    )
    return () => {
      live = false
    }
  }, [restored])
}

/**
 * Remembers the topic the chat is filtered to whenever the address changes while it shows
 * the chat; leaving for another page keeps the choice.
 */
function useRememberTopic() {
  useEffect(() => {
    const keep = () => {
      const hash = window.location.hash
      if (parse(hash).page === 'chat') writeChosenTopic(readTopicFilter(hash))
    }
    keep()
    window.addEventListener('hashchange', keep)
    return () => window.removeEventListener('hashchange', keep)
  }, [])
}

/** The chat page. `make` builds the thread for a workspace; tests give it fakes. */
export function ChatPage({ make = (id: number) => new DeskThread(id) }: { make?: (workspaceId: number) => DeskThread }) {
  // Before the thread first reads the address, so it opens on the remembered topic.
  const [restored] = useState(restoreChosenTopic)
  useDropStaleTopic(restored)
  useRememberTopic()
  const snap = useSnapshot()
  const { workspaces, workspaceId, pick } = useWorkspace()
  if (!snap.status && !snap.error) return <p className="empty">Connecting to the daemon…</p>
  return (
    <ChatThread key={workspaceId} thread={make} workspaceId={workspaceId}>
      {workspaces.length > 1 && (
        <label className="sh-conv-ws">
          <span className="sh-conv-ws-label">Workspace</span>
          <select value={workspaceId} onChange={(e) => pick(Number(e.target.value))}>
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
