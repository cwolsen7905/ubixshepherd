export function DecisionsPage({ focus }: { focus?: number }) {
  return <p className="empty">Decisions come next on this branch{focus ? ` (decision ${focus})` : ''}.</p>
}
