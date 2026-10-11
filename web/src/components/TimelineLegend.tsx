import { agentClass } from '../model/timeline'
import { href, type TimelineRange } from '../router'

/**
 * The agents, in one row above the chart, each with a bar-shaped swatch (the chart's marks
 * are bars, not dots). A link to the page filtered to that agent, so the choice is in the
 * address; the chosen one links back to all of them. The others are dimmed, not removed.
 */
export function TimelineLegend({ agents, agent, range }: { agents: string[]; agent: string; range: TimelineRange }) {
  return (
    <nav className="tl-legend" aria-label="Agents">
      {agents.map((a) => {
        const on = agent === a
        return (
          <a
            key={a}
            href={href.timeline({ range, agent: on ? undefined : a })}
            aria-current={on ? 'true' : undefined}
            title={on ? 'Show every agent' : `Show ${a} only; the others are dimmed`}
          >
            <span className={`tl-swatch tl-agent-${agentClass(a)}`} aria-hidden="true" />
            {a}
          </a>
        )
      })}
    </nav>
  )
}
