import { useEffect } from 'react'
import type { Waiting } from '../api/types'
import { faviconHref } from '../favicon'
import { attentionCount, attentionTitle } from '../state/attention'

/** The window title and the icon's lantern dot read the attention list, as the sidebar does. */
export function useAttentionChrome(waiting: Waiting | null) {
  const title = attentionTitle(waiting)
  useEffect(() => {
    document.title = title ? `${title} · Shepherd` : 'Shepherd'
  }, [title])

  const waits = attentionCount(waiting) > 0
  useEffect(() => {
    document.querySelector<HTMLLinkElement>('link[rel="icon"]')?.setAttribute('href', faviconHref(waits))
  }, [waits])
}
