import { useState } from 'react'
import { artifactContentPath } from '../api/artifacts'
import { Markdown } from '../conversation/Markdown'
import { resolveImage, splitImages } from './images'

function Image({ laneId, artifactPath, alt, src }: { laneId: number; artifactPath: string; alt: string; src: string }) {
  const [failed, setFailed] = useState(false)
  const rel = resolveImage(artifactPath, src)
  if (!rel || failed) {
    return (
      <p className="artifact-image-missing faint">
        {alt || 'image'} <code>{src}</code> {rel ? '(could not be loaded)' : '(not shown: only files of the repo are)'}
      </p>
    )
  }
  return <img className="artifact-image" src={artifactContentPath(laneId, rel)} alt={alt} loading="lazy" onError={() => setFailed(true)} />
}

/**
 * A markdown artifact: the shared renderer for the text, and each image reference shown from
 * its file in the artifact's lane. Anything not a file of the repo is not fetched. `older` says
 * the text is a past version, whose images can only be the lane's current files.
 */
export function ArtifactMarkdown({ text, laneId, path, older }: { text: string; laneId: number; path: string; older?: boolean }) {
  const pieces = splitImages(text)
  return (
    <>
      {older && pieces.some((p) => p.t === 'img') && (
        <p className="faint" role="note">Images show the lane’s current files: only this file is kept per version.</p>
      )}
      {pieces.map((p, i) =>
        p.t === 'md' ? <Markdown key={i} text={p.text} /> : <Image key={`${i}:${laneId}:${path}:${p.src}`} laneId={laneId} artifactPath={path} alt={p.alt} src={p.src} />,
      )}
    </>
  )
}
