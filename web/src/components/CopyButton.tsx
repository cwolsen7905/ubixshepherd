import { useEffect, useRef, useState } from 'react'

/** Copies text where the async clipboard API is missing (a page served over plain http): a hidden textarea and execCommand. */
function legacyCopy(text: string): boolean {
  const ta = document.createElement('textarea')
  ta.value = text
  ta.setAttribute('readonly', '')
  ta.setAttribute('aria-hidden', 'true')
  ta.style.cssText = 'position:fixed;top:0;left:0;opacity:0;pointer-events:none'
  const active = document.activeElement instanceof HTMLElement ? document.activeElement : null
  document.body.appendChild(ta)
  ta.select()
  try {
    return document.execCommand('copy')
  } catch {
    return false
  } finally {
    ta.remove()
    active?.focus()
  }
}

export async function copyText(text: string): Promise<boolean> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // Denied or unavailable: fall through to the legacy path.
    }
  }
  return legacyCopy(text)
}

const COPIED_MS = 1500

/** An icon button that copies `text` and says so for a moment. */
export function CopyButton({ text, label = 'Copy code' }: { text: string; label?: string }) {
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle')
  const timer = useRef<number | undefined>(undefined)
  useEffect(() => () => window.clearTimeout(timer.current), [])

  const onClick = async () => {
    setState((await copyText(text)) ? 'copied' : 'failed')
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setState('idle'), COPIED_MS)
  }
  const shown = state === 'copied' ? 'Copied' : state === 'failed' ? 'Copy failed' : label

  return (
    <button type="button" className={`copy-btn copy-${state}`} aria-label={shown} title={shown} onClick={() => void onClick()}>
      <svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        {state === 'copied' ? (
          <path d="M3 8.5l3.2 3.2L13 4.5" />
        ) : (
          <>
            <rect x="5.5" y="5.5" width="8" height="8" rx="1.5" />
            <path d="M10.5 5.5v-2a1 1 0 0 0-1-1h-6a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2" />
          </>
        )}
      </svg>
      <span className="copy-text" aria-hidden="true">{state === 'copied' ? 'Copied' : state === 'failed' ? 'Failed' : ''}</span>
      <span className="visually-hidden" role="status">{state === 'idle' ? '' : shown}</span>
    </button>
  )
}
