import { topicHueClass, type TopicView } from '../model/topic'
import '../styles/topics.css'

/**
 * A small coloured pill on a thread message naming the topic its turn belongs to. The
 * colour is the topic's own (`topicHueClass`), the same as its bar chip and rail row. A
 * click filters the thread to the topic, or lifts the filter when it is already on it,
 * as the bar's chip does. A long title is truncated; the full one is the hover text.
 */
export function TopicPill({
  topic, active, onFilter,
}: {
  topic: TopicView
  active: boolean
  onFilter: (id: number | null) => void
}) {
  return (
    <button
      type="button" className={`topic-pill ${topicHueClass(topic.id)}`} aria-pressed={active}
      title={topic.title} aria-label={`Topic: ${topic.title}`}
      onClick={() => onFilter(active ? null : topic.id)}
    >
      <span className="topic-pill-title">{topic.title}</span>
    </button>
  )
}
