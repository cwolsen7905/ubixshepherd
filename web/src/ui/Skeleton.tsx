import './ui.css'

/** Placeholder lines while a read loads. The group is one busy status, not a row of silent boxes. */
export function Skeleton({ lines = 1, width = '100%' }: { lines?: number; width?: string }) {
  return (
    <div role="status" aria-busy="true" aria-label="Loading" style={{ display: 'grid', gap: 'var(--s-2)' }}>
      {Array.from({ length: lines }, (_, i) => (
        <span key={i} className="ui-skeleton" style={{ width: i === lines - 1 && lines > 1 ? '60%' : width }} />
      ))}
    </div>
  )
}
