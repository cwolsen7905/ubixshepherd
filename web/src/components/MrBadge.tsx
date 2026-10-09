import type { LaneView } from '../api/types'
import type { Tone } from '../model/marks'

export interface Badge {
  /** As the terminal dock draws it: "!34 ✓". */
  text: string
  tone: Tone
  /** The same in words, for screen readers and the tooltip: "merge request !34, pipeline passed". */
  label: string
}

/**
 * A lane's merge request as a badge, with the rules of mrBadge in internal/chat/dock.go;
 * null for a lane with no mr. mr_state merged: "!N merged" ok; closed: "!N closed" muted.
 * Otherwise by pipeline_status: passed "!N ✓" ok, failed "!N ✗" bad, pending or running
 * "!N …" muted, canceled or skipped "!N canceled"/"!N skipped" muted, else "!N" muted.
 */
export function mrBadge(lane: Pick<LaneView, 'mr' | 'mr_state' | 'pipeline_status'>): Badge | null {
  const { mr, mr_state, pipeline_status } = lane
  if (mr === undefined || mr === 0) {
    return null
  }

  const mrText = `!${mr}`

  if (mr_state === 'merged') {
    return {
      text: `${mrText} merged`,
      tone: 'ok',
      label: `merge request ${mrText}, merged`
    }
  }

  if (mr_state === 'closed') {
    return {
      text: `${mrText} closed`,
      tone: 'muted',
      label: `merge request ${mrText}, closed`
    }
  }

  if (pipeline_status === 'passed') {
    return {
      text: `${mrText} ✓`,
      tone: 'ok',
      label: `merge request ${mrText}, pipeline passed`
    }
  }

  if (pipeline_status === 'failed') {
    return {
      text: `${mrText} ✗`,
      tone: 'bad',
      label: `merge request ${mrText}, pipeline failed`
    }
  }

  if (pipeline_status === 'pending' || pipeline_status === 'running') {
    return {
      text: `${mrText} …`,
      tone: 'muted',
      label: `merge request ${mrText}, pipeline ${pipeline_status}`
    }
  }

  if (pipeline_status === 'canceled' || pipeline_status === 'skipped') {
    const status = pipeline_status === 'canceled' ? 'canceled' : 'skipped'
    return {
      text: `${mrText} ${status}`,
      tone: 'muted',
      label: `merge request ${mrText}, pipeline ${status}`
    }
  }

  // Anything else, including no pipeline_status
  return {
    text: mrText,
    tone: 'muted',
    label: `merge request ${mrText}`
  }
}

/**
 * The badge as a link to the MR when the lane has mr_url (opens in a new tab), else a
 * span; nothing without an mr. Class "badge tone-<tone>". link false keeps it a span, for
 * a badge inside something that is already a link.
 */
export function MrBadge({ lane, link = true }: { lane: Pick<LaneView, 'mr' | 'mr_state' | 'pipeline_status' | 'mr_url'>; link?: boolean }) {
  const badge = mrBadge(lane)
  if (badge === null) {
    return null
  }

  const { text, tone } = badge
  const className = `badge tone-${tone}`

  if (link && lane.mr_url) {
    return (
      <a
        href={lane.mr_url}
        target="_blank"
        rel="noreferrer"
        className={className}
        title={badge.label}
        aria-label={badge.label}
      >
        {text}
      </a>
    )
  } else {
    return (
      <span
        className={className}
        title={badge.label}
        aria-label={badge.label}
      >
        {text}
      </span>
    )
  }
}