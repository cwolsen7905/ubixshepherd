import { useEffect, useState } from 'react'
import { topics as topicsApi } from '../api/client'
import { ErrorBoundary } from '../components/ErrorBoundary'
import type { DeskThread } from '../state/desk'
import { DeskThread as Desk } from '../state/desk'
import { Skeleton } from '../ui'
import { ChatThread } from './thread'
import { useWorkspace } from './workspace'

const defaultMake = (id: number) => new Desk(id)

/**
 * The topic a decision belongs to: the one that links it, or null when none does. `undefined`
 * while the daemon has not said, so the column does not flash the whole thread first.
 */
export function useDecisionTopic(decisionId: number): number | null | undefined {
  const [got, setGot] = useState<{ id: number; topic: number | null } | null>(null)
  useEffect(() => {
    let live = true
    topicsApi.list(true).then(
      (l) => live && setGot({ id: decisionId, topic: l.topics.find((t) => t.status === 'open' && t.links.some((k) => k.kind === 'decision' && k.ref === String(decisionId)))?.id ?? null }),
      () => live && setGot({ id: decisionId, topic: null }),
    )
    return () => {
      live = false
    }
  }, [decisionId])
  return got?.id === decisionId ? got.topic : undefined
}

/**
 * The thread in the side panel while a decision fills the main column (WEB-012): the same
 * conversation, held to the decision's topic, with its composer. `]` belongs to the page beside.
 */
export function SwapThread({ decisionId, make = defaultMake, keys = false }: { decisionId: number; make?: (workspaceId: number) => DeskThread; keys?: boolean }) {
  const { workspaceId } = useWorkspace()
  const topic = useDecisionTopic(decisionId)
  if (topic === undefined) return <Skeleton lines={4} />
  return (
    <ErrorBoundary region="The conversation">
      <ChatThread key={`${workspaceId}:${topic}`} thread={make} workspaceId={workspaceId} side topic={topic} keys={keys} />
    </ErrorBoundary>
  )
}
