import { useState } from 'react'
import type { Status } from '../api/types'
import { useSnapshot } from '../state/context'

const KEY = 'shepherd.chat.workspace'

function read(): number {
  try {
    return Number(window.localStorage.getItem(KEY)) || 0
  } catch {
    return 0
  }
}

/**
 * The workspace the conversation is about. The daemon takes a workspace_id only when it has more
 * than one, so with one this is 0. The pick is remembered, so the thread beside a decision is the
 * one the Conversation shows.
 */
export function useWorkspace(): { workspaces: Status['workspaces']; workspaceId: number; pick: (id: number) => void } {
  const snap = useSnapshot()
  const workspaces = snap.status?.workspaces ?? []
  const [picked, setPicked] = useState(read)
  const workspaceId = workspaces.length > 1 ? (workspaces.find((w) => w.id === picked) ?? workspaces[0])?.id ?? 0 : 0
  return {
    workspaces,
    workspaceId,
    pick: (id) => {
      setPicked(id)
      try {
        window.localStorage.setItem(KEY, String(id))
      } catch {
        // The pick is only a convenience.
      }
    },
  }
}
