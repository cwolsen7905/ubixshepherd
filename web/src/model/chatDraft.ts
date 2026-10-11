// Half-typed words in the chat page, kept so leaving the page and coming back loses nothing.
// The store is passed in (as readView in projects.ts) so tests give it a fake.

type Reader = Pick<Storage, 'getItem'> | undefined
type Writer = Pick<Storage, 'setItem' | 'removeItem'> | undefined

/** The composer's draft is kept per workspace: 0 is the daemon's only workspace. */
export const draftKey = (workspaceId: number) => `shepherd.chat.draft.${workspaceId}`

/** The answer being written to a decision, kept per decision. */
export const answerKey = (decisionId: number) => `shepherd.chat.answer.${decisionId}`

/** The stored words under `key`, or '' when there are none or the browser refuses storage. */
export function readDraft(store: Reader, key: string): string {
  try {
    return store?.getItem(key) ?? ''
  } catch {
    return ''
  }
}

/** Keeps `text` under `key`; empty text clears it, so nothing blank is left behind. */
export function writeDraft(store: Writer, key: string, text: string) {
  try {
    if (text === '') store?.removeItem(key)
    else store?.setItem(key, text)
  } catch {
    // A browser that refuses storage forgets the draft; nothing else depends on it.
  }
}

export function clearDraft(store: Pick<Storage, 'removeItem'> | undefined, key: string) {
  try {
    store?.removeItem(key)
  } catch {
    // As writeDraft.
  }
}
