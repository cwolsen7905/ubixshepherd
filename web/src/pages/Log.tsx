import { useEffect } from 'react'
import { BackLink } from '../components/BackLink'
import { RunInspector } from '../inspector/runPane'

/** The run page: the same Log/Changes/Artifacts view as the run inspector, full width. */
export function LogPage({ run: id }: { run: number }) {
  useEffect(() => {
    document.title = `Run ${id} · Shepherd`
    return () => {
      document.title = 'Shepherd'
    }
  }, [id])
  return (
    <div className="page log-page">
      <BackLink />
      <RunInspector id={id} full />
    </div>
  )
}
