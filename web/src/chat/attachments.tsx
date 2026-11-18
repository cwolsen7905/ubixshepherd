import { deskApi } from '../api/desk'
import type { DeskAttachment } from '../api/types'

/** The limits the daemon holds (internal/attach); the box checks them first for a quick answer. */
export const MAX_FILE_BYTES = 10 * 1024 * 1024
export const MAX_FILES = 5
export const MAX_TURN_BYTES = 25 * 1024 * 1024

/** Program, script and archive extensions the daemon refuses. */
const REFUSED_EXT = /\.(exe|dll|msi|app|bat|cmd|com|scr|sh|bash|zsh|ps1|vbs|jar|dmg|pkg|so|dylib|apk|deb|rpm|zip|tar|gz|tgz|bz2|xz|7z|rar)$/i

export function sizeText(n: number): string {
  if (n >= 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MiB`
  if (n >= 1024) return `${Math.ceil(n / 1024)} KiB`
  return `${n} bytes`
}

/** Why a file cannot go, in words, or null when it can. `kept` is how many files and bytes the message already holds. */
export function refusal(file: { name: string; size: number }, kept: { count: number; bytes: number }): string | null {
  if (file.size === 0) return `${file.name} is empty.`
  if (REFUSED_EXT.test(file.name)) return `${file.name} is a program, script or archive, which the desk does not take.`
  if (file.size > MAX_FILE_BYTES) return `${file.name} is over the ${MAX_FILE_BYTES / (1024 * 1024)} MiB limit.`
  if (kept.count >= MAX_FILES) return `A message takes at most ${MAX_FILES} files.`
  if (kept.bytes + file.size > MAX_TURN_BYTES) return `A message takes at most ${MAX_TURN_BYTES / (1024 * 1024)} MiB of files.`
  return null
}

/** `▤` marks a file that is not shown as an image. */
function FileChip({ f }: { f: DeskAttachment }) {
  return (
    <a className="sh-chip sh-attach-chip" href={deskApi.attachmentUrl(f.id)} download={f.name} title={`${f.name}, ${f.mime}`}>
      <span aria-hidden="true">▤ </span>
      <span className="sh-attach-name">{f.name}</span>
      <span className="sh-attach-size"> {sizeText(f.size)}</span>
    </a>
  )
}

/** The files a message carried: an image inline (a click opens it whole), any other file a chip with its name and size. */
export function AttachmentList({ files }: { files: DeskAttachment[] }) {
  return (
    <div className="sh-attach-list" data-component="AttachmentList">
      {files.map((f) =>
        f.kind === 'image' ? (
          <a key={f.id} className="sh-attach-image" href={deskApi.attachmentUrl(f.id)} target="_blank" rel="noreferrer" title={`${f.name}, ${sizeText(f.size)}`}>
            <img src={deskApi.attachmentUrl(f.id)} alt={f.name} loading="lazy" />
          </a>
        ) : (
          <FileChip key={f.id} f={f} />
        ),
      )}
    </div>
  )
}
