import { useCallback, useEffect, useMemo, useState, forwardRef, useImperativeHandle } from 'react'
import { deskApi } from '../api/desk'
import { topics as topicsApi } from '../api/client'
import { usePaletteKey } from '../hooks'
import { go, href } from '../router'
import { useSnapshot } from '../state/context'
import type { TopicView } from '../model/topic'
import { buildItems, type Item } from './items'
import { Palette } from './Palette'

export interface PaletteHandle {
  /** Open the palette. */
  open: () => void
}

/**
 * The command palette, mounted once. Cmd+K or Ctrl+K toggles it; topics are read when it
 * opens, the rest comes from the live snapshot. Exposes `open` via ref so the sidebar can
 * trigger it.
 */
export const CommandPalette = forwardRef<PaletteHandle>(function CommandPalette(_props, ref) {
  const snap = useSnapshot()
  const [open, setOpen] = useState(false)
  const [topics, setTopics] = useState<TopicView[]>([])
  const [confirming, setConfirming] = useState(false)
  const [problem, setProblem] = useState('')
  const close = useCallback(() => {
    setOpen(false)
    setConfirming(false)
    setProblem('')
  }, [])

  useImperativeHandle(ref, () => ({ open: () => setOpen(true) }), [])

  usePaletteKey(useCallback(() => (open ? close() : setOpen(true)), [open, close]))

  useEffect(() => {
    if (!open) return
    let live = true
    topicsApi.list(false).then(
      (l) => live && setTopics(l.topics),
      () => undefined,
    )
    return () => {
      live = false
    }
  }, [open])

  const items = useMemo(() => buildItems(snap, topics), [snap, topics])
  const workspaces = snap.status?.workspaces ?? []

  const choose = (it: Item) => {
    if ('to' in it.target) {
      close()
      go(it.target.to)
      return
    }
    // The daemon takes a workspace only when it has several; with several, the chat page
    // has the picker and its own confirmation.
    if (workspaces.length > 1) {
      close()
      go(href.chat())
      return
    }
    setConfirming(true)
  }

  const start = () => {
    deskApi.newConversation(0).then(
      () => {
        close()
        go(href.chat())
      },
      (e: unknown) => {
        setConfirming(false)
        setProblem(`Could not start a new conversation: ${e instanceof Error ? e.message : String(e)}`)
      },
    )
  }

  return (
    <Palette
      open={open}
      items={items}
      onClose={close}
      onChoose={choose}
      confirm={
        confirming
          ? {
              title: 'Start a new conversation? The desk forgets this thread.',
              yes: 'start new',
            }
          : null
      }
      onConfirm={start}
      onCancelConfirm={() => setConfirming(false)}
      problem={problem}
    />
  )
})
