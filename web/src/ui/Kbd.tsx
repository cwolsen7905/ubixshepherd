/** A key hint (`.sh-kbd`). Pass a chord as separate keys: `<Kbd keys={['g', 'i']} />`. */
export function Kbd({ keys }: { keys: string | string[] }) {
  const list = Array.isArray(keys) ? keys : [keys]
  return (
    <>
      {list.map((k, i) => (
        <kbd key={i} className="sh-kbd" data-component="Kbd">
          {k}
        </kbd>
      ))}
    </>
  )
}
