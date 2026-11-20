import { Button, Dialog, Kbd } from '../ui'
import { KEYS } from '../lib/keys'
import { Legend } from './Legend'

/** The keys and glyphs sheet: a modal with the keys table and the glyph legend. `?` or Esc closes it. */
export function Help({ onClose }: { onClose: () => void }) {
  return (
    <Dialog
      open
      onClose={onClose}
      title="Keys"
      size="wide"
      hint={false}
      actions={
        <>
          <Button onClick={onClose}>Close</Button>
          <span className="sh-modal-hint">
            <Kbd keys="?" />
            or
            <Kbd keys="Esc" />
            to close
          </span>
        </>
      }
    >
      <dl className="sh-keys">
        {KEYS.map((k) => (
          <div key={k.combos.map((c) => c.join(' ')).join('|')} className="sh-keys-row">
            <dt>
              {k.combos.map((c, i) => (
                <span key={i}>
                  {i > 0 && ' / '}
                  <Kbd keys={c} />
                </span>
              ))}
            </dt>
            <dd>{k.what}</dd>
          </div>
        ))}
      </dl>
      <h2>Glyphs</h2>
      <Legend />
    </Dialog>
  )
}
