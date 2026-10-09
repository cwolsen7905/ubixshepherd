// Formatting helpers: durations, relative times, cost. Pure functions, no React.

/** A short age like the terminal dock's: "40s" under a minute, "3m" under an hour, "2h" under 48 hours, else "4d". Negative is "0s". Whole units, rounded down. */
export function age(ms: number): string {
  void ms
  throw new Error('not implemented')
}

/** How long ago an RFC 3339 time was, as age(): "3m". "" for an empty or unparsable time, or one in the future. */
export function since(iso: string | undefined, now: number): string {
  void iso; void now
  throw new Error('not implemented')
}

/** A run's length from start to end (or to now while running): "38s", "4m 12s", "1h 04m" (minutes zero-padded after hours). "" when start is unparsable. */
export function duration(startIso: string, endIso: string | undefined, now: number): string {
  void startIso; void endIso; void now
  throw new Error('not implemented')
}

/** Dollars: "$0.00", "$1.24", "$12.50"; "<$0.01" for a positive amount under a cent; whole dollars from $100 ("$123"). */
export function usd(n: number): string {
  void n
  throw new Error('not implemented')
}

/** What a run cost: "$1.24", "3.5 credits", "1 credit", both "$1.24 + 3 credits"; "" when neither is set or both are zero. Credits keep at most one decimal. */
export function runCost(run: { cost_usd?: number; credits?: number }): string {
  void run
  throw new Error('not implemented')
}

/** A time of day for a timeline: "14:05" when the time is on now's local day, else "Oct 3 14:05" (en-US short month, 24-hour). "" for unparsable. */
export function clockTime(iso: string, now: number): string {
  void iso; void now
  throw new Error('not implemented')
}

/** The first 8 characters of a commit SHA; "" for undefined. */
export function shortSha(sha: string | undefined): string {
  void sha
  throw new Error('not implemented')
}
