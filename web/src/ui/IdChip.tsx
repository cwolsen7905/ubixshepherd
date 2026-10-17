import { refLabel, type IdKind } from '../state/ids'
import './ui.css'

export type { IdKind }

/**
 * A typed ID ("decision 275", `.ui-idchip`). With `bare` it shows only the number
 * ("311") while the label stays on `aria-label` and `title`. With `href` it is a link;
 * otherwise a plain span the person can select in one go.
 */
export function IdChip({ kind, id, bare, href }: { kind: IdKind; id: number; bare?: boolean; href?: string }) {
  const label = refLabel(kind, id)
  const text = bare ? String(id) : label
  if (href) {
    return (
      <a className="ui-idchip" data-component="IdChip" href={href} aria-label={label} title={label}>
        {text}
      </a>
    )
  }
  return (
    <span className="ui-idchip" data-component="IdChip" aria-label={bare ? label : undefined} title={label}>
      {text}
    </span>
  )
}
