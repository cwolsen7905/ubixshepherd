import { Button, Dialog, Kbd } from '../ui'
import { Legend } from './Legend'

const KEYS: [string, string][] = [
  ['Ctrl+K / ⌘K', 'Search or act: go to a page, open a lane, run or decision'],
  ['j / k', 'Move down and up the board'],
  ['Enter', 'Open the lane or decision'],
  ['g b', 'Go to the board'],
  ['g d', 'Go to decisions'],
  ['g t', 'Go to timeline'],
  ['g n', 'Go to next up'],
  ['c', 'Mark everything done as seen'],
  ['/', 'Search a run log'],
  ['f', 'Follow a run log as it grows'],
  ['?', 'Show or hide these keys'],
]

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
        {KEYS.map(([k, what]) => (
          <div key={k} className="sh-keys-row">
            <dt>
              {k.split(' ').map((p) => (p === '/' && k !== '/' ? ' / ' : <Kbd key={p} keys={p} />))}
            </dt>
            <dd>{what}</dd>
          </div>
        ))}
      </dl>
      <h2>Glyphs</h2>
      <Legend />
    </Dialog>
  )
}
