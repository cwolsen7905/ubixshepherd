import type { ReactNode } from 'react'
import { cx } from './cx'

/**
 * The view icons: one line drawing per view, on a 16 by 16 grid, in one stroke width with round
 * caps and joins, drawn in the current text colour. They name places, not states: statuses keep
 * the glyph language (StatusGlyph). Each is decoration beside its words, so it is hidden from a
 * screen reader.
 */
const PATHS = {
  inbox: (
    <>
      <path d="M2 9.5h3.25l1 1.75h3.5l1-1.75H14" />
      <path d="M4 3h8l2 6.5V13H2V9.5z" />
    </>
  ),
  chat: <path d="M2.5 3.5h11v7.5h-6l-3 2.5V11h-2z" />,
  board: (
    <>
      <rect x="2" y="2.5" width="12" height="11" rx="1.5" />
      <path d="M6 2.5v11M10 2.5v11" />
    </>
  ),
  next: (
    <>
      <path d="M2.5 3.5 7 8l-4.5 4.5" />
      <path d="M8.5 3.5 13 8l-4.5 4.5" />
    </>
  ),
  artifacts: (
    <>
      <path d="M3.5 1.75h5.5l3.5 3.5v9h-9z" />
      <path d="M9 1.75v3.5h3.5M6 9h4M6 11.5h4" />
    </>
  ),
  files: <path d="M2 3h4.25l1.5 2H14v8H2z" />,
  runs: (
    <>
      <circle cx="8" cy="8" r="5.75" />
      <path d="M6.75 5.5 10.5 8l-3.75 2.5z" />
    </>
  ),
  timeline: (
    <>
      <path d="M1.75 8h2M6.25 8h3.5M12.25 8h2" />
      <circle cx="5" cy="8" r="1.25" />
      <circle cx="11" cy="8" r="1.25" />
    </>
  ),
  history: (
    <>
      <path d="M2.5 8a5.5 5.5 0 1 0 1.6-3.9L2.5 5.75" />
      <path d="M2.5 2.5v3.25h3.25M8 5v3l2 1.5" />
    </>
  ),
  projects: (
    <>
      <path d="M8 2 13.5 5 8 8 2.5 5z" />
      <path d="M2.5 8 8 11l5.5-3M2.5 11 8 14l5.5-3" />
    </>
  ),
  rules: (
    <>
      <path d="M8 2.5v11M5 13.5h6M3.5 4.5h9" />
      <path d="M3.5 4.5 1.75 9a1.75 1.75 0 0 0 3.5 0zM12.5 4.5 10.75 9a1.75 1.75 0 0 0 3.5 0z" />
    </>
  ),
  classifiers: (
    <>
      <path d="M2.25 2.25h5.5l6 6-5.5 5.5-6-6z" />
      <circle cx="5.5" cy="5.5" r="1" />
    </>
  ),
  guardrails: <path d="M8 1.75 13.25 3.75V8c0 3-2.25 5.25-5.25 6.25C5 13.25 2.75 11 2.75 8V3.75z" />,
  reviewbots: (
    <>
      <rect x="2.5" y="5" width="11" height="8.5" rx="1.5" />
      <path d="M8 2.25V5M6 8.5v1.25M10 8.5v1.25" />
    </>
  ),
  usage: <path d="M2 13.5h12M4.5 11V8M8 11V3.5M11.5 11V6" />,
  settings: (
    <>
      <path d="M2 4h2.5M7.5 4H14M2 8h6.5M11.5 8H14M2 12h1.5M6.5 12H14" />
      <circle cx="6" cy="4" r="1.5" />
      <circle cx="10" cy="8" r="1.5" />
      <circle cx="5" cy="12" r="1.5" />
    </>
  ),
  status: <path d="M1.5 8.5h3l1.75-4.5 3.5 8 1.75-3.5h3" />,
  search: (
    <>
      <circle cx="7" cy="7" r="4.5" />
      <path d="m10.5 10.5 3.5 3.5" />
    </>
  ),
  'chevron-left': <path d="M10 3.5 5.5 8l4.5 4.5" />,
  'chevron-right': <path d="M6 3.5 10.5 8 6 12.5" />,
  'keep-going': (
    <>
      <path d="M2.75 8A5.25 5.25 0 0 1 11.75 4.25" />
      <path d="M12 1.75v2.75H9.25" />
      <path d="M13.25 8A5.25 5.25 0 0 1 4.25 11.75" />
      <path d="M4 14.25V11.5h2.75" />
    </>
  ),
} satisfies Record<string, ReactNode>

export type IconName = keyof typeof PATHS

/** Every icon's name, for a test or a gallery. */
export const ICON_NAMES = Object.keys(PATHS) as IconName[]

/** One icon (`.sh-icon`): 16 by 16, stroked in currentColor, hidden from assistive tech. */
export function Icon({ name, className }: { name: IconName; className?: string }) {
  return (
    <svg
      className={cx('sh-icon', className)}
      data-component="Icon"
      data-icon={name}
      viewBox="0 0 16 16"
      width="16"
      height="16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      {PATHS[name]}
    </svg>
  )
}
