import { EmptyState } from '../ui'

/** A deliberate placeholder: the nav lists Guardrails so the place is known before the page exists. */
export function GuardrailsPage() {
  return (
    <div className="page" data-component="Guardrails">
      <header className="page-head">
        <h1>Guardrails</h1>
      </header>
      <EmptyState title="Guardrails are not built yet">This page is a placeholder.</EmptyState>
    </div>
  )
}
