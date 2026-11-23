import { EmptyState } from '../ui'

/** A deliberate placeholder: the nav lists Classifiers so the place is known before the page exists. */
export function ClassifiersPage() {
  return (
    <div className="page" data-component="Classifiers">
      <header className="page-head">
        <h1>Classifiers</h1>
      </header>
      <EmptyState title="Classifiers are not built yet">This page is a placeholder.</EmptyState>
    </div>
  )
}
