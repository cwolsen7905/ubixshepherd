import type { ReactNode } from 'react'
import { cx } from './cx'
import './ArtifactView.css'

export type ArtifactViewLayout = 'page' | 'inline'

export interface ArtifactViewProps {
  /** `page`: the artifact's own page, with its bar. `inline`: the content alone, in another page (a decision's plan). */
  layout?: ArtifactViewLayout
  /** `page`: the heading. */
  title?: ReactNode
  /** `page`: the file's path, in mono. */
  path?: string
  /** `page`: what Shepherd does with the file ("kept local", "untracked"). */
  tag?: ReactNode
  tagTone?: 'local' | 'warn' | 'plain'
  /** `page`: run, type, size, age and lane, as links and words. */
  meta?: ReactNode
  /** `page`: Source, Copy, Open raw, Pin, Keep and the rest. */
  actions?: ReactNode
  /** `page`: the way back, as a link or, with `onBack`, a button. */
  backLabel?: string
  backHref?: string
  onBack?: () => void
  /** `page`: a problem or a note under the actions. */
  notice?: ReactNode
  /** The content: a rendered document, an image, code. */
  children: ReactNode
}

/**
 * One artifact (`.sh-artifact`). As a page: back link, heading, path with its tag, a meta line,
 * the actions over a rule, then the content. Inline: just the content, so a decision can show the
 * plan it links to without leaving. The content is the caller's, because what renders it
 * (stored text, a lane's file, an image) differs; this part gives it the document frame.
 */
export function ArtifactView({
  layout = 'page',
  title,
  path,
  tag,
  tagTone = 'plain',
  meta,
  actions,
  backLabel = '← Artifacts',
  backHref,
  onBack,
  notice,
  children,
}: ArtifactViewProps) {
  if (layout === 'inline') {
    return (
      <div className="sh-artifact sh-artifact--inline" data-component="ArtifactView" data-layout="inline">
        <div className="sh-artifact-body">{children}</div>
      </div>
    )
  }
  const back = onBack ? (
    <button type="button" className="sh-artifact-back" onClick={onBack}>
      {backLabel}
    </button>
  ) : backHref ? (
    <a className="sh-artifact-back" href={backHref}>
      {backLabel}
    </a>
  ) : null
  return (
    <div className="sh-artifact" data-component="ArtifactView" data-layout="page">
      {back}
      {title && <h1 className="sh-artifact-title">{title}</h1>}
      {path && (
        <div className="sh-artifact-path">
          <code>{path}</code>
          {tag && <span className={cx('sh-tag', tagTone === 'local' && 'sh-tag--local', tagTone === 'warn' && 'sh-tag--warn')}>{tag}</span>}
        </div>
      )}
      {meta && <div className="sh-artifact-meta">{meta}</div>}
      {actions && (
        <div className="sh-artifact-actions" role="group" aria-label="What to do with this file">
          {actions}
        </div>
      )}
      {notice}
      <div className="sh-artifact-body">{children}</div>
    </div>
  )
}
