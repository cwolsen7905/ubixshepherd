import { useState } from 'react'
import type { LaneView } from '../api/types'
import { LaneCloseButton } from '../components/LaneClose'
import { Button } from './Button'
import { Dialog } from './Dialog'

/**
 * The header's destructive action: "Close lane…" is an outlined button that opens a dialog, and
 * the lane's own close (its checks, its refusal and "Close anyway…") runs inside it. Disabled
 * while a run is going in the lane.
 */
export function CloseLaneAction({ lane, running }: { lane: LaneView; running: boolean }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <Button disabled={running} title={running ? 'A run is going in this lane: stop it first.' : undefined} onClick={() => setOpen(true)}>
        Close lane…
      </Button>
      <Dialog
        open={open}
        onClose={() => setOpen(false)}
        title={`Close ${lane.name}?`}
        alert
        size="wide"
        actions={
          <Button variant="ghost" onClick={() => setOpen(false)}>
            Keep the lane
          </Button>
        }
      >
        <p>
          Closing removes the worktree <code>{lane.worktree}</code> if it is clean and its work is in the base. If it is not, you are told why and can decide.
        </p>
        <LaneCloseButton lane={lane} running={running} onClosed={() => setOpen(false)} />
      </Dialog>
    </>
  )
}
