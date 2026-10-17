import { useEffect } from 'react'
import { BackLink, pageExit } from '../components/BackLink'
import { RunInspector } from '../inspector/runPane'

/** The run page: the same Log/Changes/Artifacts view as the run inspector, full width, under the shared page header. */
export function LogPage({ run: id }: { run: number }) {
  useEffect(() => {
    document.title = `Run ${id} · Shepherd`
    return () => {
      document.title = 'Shepherd'
    }
  }, [id])
  return (
    <div className="page log-page">
      <RunInspector id={id} full chrome={{ back: <BackLink />, ...pageExit({ kind: 'run', id }) }} />
    </div>
  )
}
