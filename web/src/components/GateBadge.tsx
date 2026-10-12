import type { GateBadge as Gate, GateState } from '../api/types'
import { clockTime, shortSha } from '../format'

const WORDS: Record<GateState, string> = {
  passed: 'gate passed',
  failed: 'gate failed',
  stale: 'gate stale',
  not_run: 'gate not run',
}

const MEANING: Record<GateState, string> = {
  passed: "Shepherd's gate passed on this head.",
  failed: "Shepherd's gate failed on this head.",
  stale: "The gate last ran on an earlier commit: there are commits since.",
  not_run: "Shepherd's gate has not run here. That is normal for a repo where a person pushes.",
}

const TONES: Record<GateState, string> = { passed: 'ok', failed: 'bad', stale: 'warn', not_run: 'muted' }

/** The badge's hover text: what the state means, then the record's commit, time and command. */
export function gateTitle(gate: Gate, now?: number): string {
  const parts = [MEANING[gate.state] ?? gate.state]
  if (gate.sha) parts.push(`commit ${shortSha(gate.sha)}`)
  if (gate.at) parts.push(`at ${now === undefined ? gate.at : clockTime(gate.at, now) || gate.at}`)
  if (gate.command) parts.push(`command: ${gate.command}`)
  if (gate.source) parts.push(`source: ${gate.source}`)
  return parts.join('\n')
}

/**
 * A lane's gate record as a small pill: passed, failed, stale or not run, with the commit,
 * time and command on hover. Nothing when the daemon sent no record. "Not run" is muted,
 * not a failure.
 */
export function GateBadge({ gate, now }: { gate?: Gate | null; now?: number }) {
  if (!gate) return null
  const title = gateTitle(gate, now)
  const word = WORDS[gate.state] ?? `gate ${gate.state}`
  return (
    <span className={`badge gate-badge gate-${gate.state} tone-${TONES[gate.state] ?? 'muted'}`} title={title} aria-label={`${word}. ${title.replace(/\n/g, '. ')}`}>
      {word}
    </span>
  )
}
