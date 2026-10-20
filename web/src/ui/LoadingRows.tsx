import { useEffect, useState } from 'react'
import './ui.css'

/** How long a read may take before its placeholder shows: a quick read never flashes one. */
export const LOADING_DELAY_MS = 300

/**
 * Placeholder rows in the real row grid while a list loads. They appear after 300 ms, so a read
 * that returns at once shows nothing in between. The group is one busy status.
 */
export function LoadingRows({ rows = 3, delay = LOADING_DELAY_MS }: { rows?: number; delay?: number }) {
  const [shown, setShown] = useState(delay <= 0)
  useEffect(() => {
    if (delay <= 0) return
    const t = setTimeout(() => setShown(true), delay)
    return () => clearTimeout(t)
  }, [delay])
  if (!shown) return null
  return (
    <div role="status" aria-busy="true" aria-label="Loading" className="ui-loadrows" data-component="LoadingRows">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="ui-loadrow" aria-hidden="true">
          <span className="ui-skeleton ui-loadrow-glyph" />
          <span className="ui-skeleton ui-loadrow-title" />
          <span className="ui-skeleton ui-loadrow-meta" />
        </div>
      ))}
    </div>
  )
}
