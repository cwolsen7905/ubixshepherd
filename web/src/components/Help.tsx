import { useEffect, useRef } from 'react'

const KEYS: [string, string][] = [
  ['j / k', 'Move down and up the board'],
  ['Enter', 'Open the lane or decision'],
  ['g b', 'Go to the board'],
  ['g d', 'Go to decisions'],
  ['c', 'Mark everything done as seen'],
  ['/', 'Search a run log'],
  ['f', 'Follow a run log as it grows'],
  ['?', 'Show or hide these keys'],
]

export function Help({ onClose }: { onClose: () => void }) {
  const ref = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const d = ref.current
    if (d && !d.open) d.showModal?.()
  }, [])
  return (
    <dialog ref={ref} className="help" onClose={onClose} onCancel={onClose} aria-labelledby="help-title">
      <h2 id="help-title">Keys</h2>
      <dl>
        {KEYS.map(([k, what]) => (
          <div key={k}>
            <dt>
              {k.split(' ').map((p) => (p === '/' && k !== '/' ? ' / ' : <kbd key={p}>{p}</kbd>))}
            </dt>
            <dd>{what}</dd>
          </div>
        ))}
      </dl>
      <form method="dialog">
        <button type="submit" className="button">Close</button>
      </form>
    </dialog>
  )
}
