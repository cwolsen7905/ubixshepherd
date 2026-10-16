import '../styles/components/unseen.css'

/** A small mark for something that waits on the person and has not been opened. */
export function UnseenDot() {
  return (
    <span className="unseen-dot" role="img" aria-label="unseen">
      <span aria-hidden="true">●</span>
    </span>
  )
}
