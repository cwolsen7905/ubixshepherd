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
  void lane
  throw new Error('not implemented')
}

/** The badge as a link to the MR when the lane has mr_url (opens in a new tab), else a span; nothing without an mr. Class "badge tone-<tone>". */
export function MrBadge({ lane }: { lane: Pick<LaneView, 'mr' | 'mr_state' | 'pipeline_status' | 'mr_url'> }) {
  void lane
  return null
}
