import { refLabel, type IdKind } from '../state/ids'
import './ui.css'

export type { IdKind }

/**
 * A typed ID ("decision 275", `.ui-idchip`). With `bare` it shows only the number
 * ("311") while the full label stays readable by assistive tech and on `title`.
 * With `href` it is a link; otherwise a plain span the person can select in one go.
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
  if (bare) {
    return (
      <span className="ui-idchip" data-component="IdChip" title={label}>
        <span aria-hidden="true">{text}</span>
        <span className="visually-hidden">{label}</span>
      </span>
    )
  }
  return (
    <span className="ui-idchip" data-component="IdChip" title={label}>
      {text}
    </span>
  )
}
