// Formatting helpers: durations, relative times, cost. Pure functions, no React.

/** A short age like the terminal dock's: "40s" under a minute, "3m" under an hour, "2h" under 48 hours, else "4d". Negative is "0s". Whole units, rounded down. */
export function age(ms: number): string {
  if (ms < 0) return '0s'
  
  const seconds = Math.floor(ms / 1000)
  if (seconds < 60) return `${seconds}s`
  
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  
  const hours = Math.floor(minutes / 60)
  if (hours < 48) return `${hours}h`
  
  const days = Math.floor(hours / 24)
  return `${days}d`
}

/** How long ago an RFC 3339 time was, as age(): "3m". "" for an empty or unparsable time, or one in the future. */
export function since(iso: string | undefined, now: number): string {
  if (!iso) return ''
  
  const date = new Date(iso)
  if (isNaN(date.getTime())) return ''
  
  const diff = now - date.getTime()
  if (diff < 0) return '' // Future time
  
  return age(diff)
}

/** A run's length from start to end (or to now while running): "38s", "4m 12s", "1h 04m" (minutes zero-padded after hours). "" when start is unparsable. */
export function duration(startIso: string, endIso: string | undefined, now: number): string {
  const startDate = new Date(startIso)
  if (isNaN(startDate.getTime())) return ''
  
  const endDate = endIso ? new Date(endIso) : new Date(now)
  
  const diffMs = endDate.getTime() - startDate.getTime()
  const diffSeconds = Math.floor(diffMs / 1000)
  
  if (diffSeconds < 0) return ''
  
  const hours = Math.floor(diffSeconds / 3600)
  const minutes = Math.floor((diffSeconds % 3600) / 60)
  const seconds = diffSeconds % 60
  
  if (hours > 0) {
    // Format as "1h 04m" with zero-padded minutes after hours
    return `${hours}h ${minutes.toString().padStart(2, '0')}m`
  } else if (minutes > 0) {
    return `${minutes}m ${seconds}s`
  } else {
    return `${seconds}s`
  }
}

/** Dollars: "$0.00", "$1.24", "$12.50"; "<$0.01" for a positive amount under a cent; whole dollars from $100 ("$123"). */
export function usd(n: number): string {
  if (n < 0) return ''
  
  if (n > 0 && n < 0.01) return '<$0.01'
  
  if (n >= 100) {
    // For values $100 and up, show whole dollars
    return `$${Math.floor(n)}`
  }
  
  // Format with 2 decimal places for amounts less than $100
  return `$${n.toFixed(2)}`
}

/** What a run cost: "$1.24", "3.5 credits", "1 credit", both "$1.24 + 3 credits"; "" when neither is set or both are zero. Credits keep at most one decimal. */
export function runCost(run: { cost_usd?: number; credits?: number }): string {
  const usdValue = run.cost_usd || 0
  const creditsValue = run.credits || 0
  
  if (usdValue === 0 && creditsValue === 0) return ''
  
  let result = ''
  
  if (usdValue > 0) {
    result += usd(usdValue)
  }
  
  if (creditsValue > 0) {
    if (result) result += ' + '
    const creditsStr = creditsValue % 1 === 0 ? `${creditsValue} credits` : `${creditsValue.toFixed(1)} credits`
    result += creditsStr
  }
  
  return result
}

/** A time of day for a timeline: "14:05" when the time is on now's local day, else "Oct 3 14:05" (en-US short month, 24-hour). "" for unparsable. */
export function clockTime(iso: string, now: number): string {
  const date = new Date(iso)
  if (isNaN(date.getTime())) return ''
  
  const nowDate = new Date(now)
  
  // Check if the date is on the same local day as now
  if (
    date.getFullYear() === nowDate.getFullYear() &&
    date.getMonth() === nowDate.getMonth() &&
    date.getDate() === nowDate.getDate()
  ) {
    // Same day - show just the time
    return date.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false })
  } else {
    // Different day - show month, day, and time
    return date.toLocaleString('en-US', {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false
    }).replace(/,\s*/, ' ')
  }
}

/** The first 8 characters of a commit SHA; "" for undefined. */
export function shortSha(sha: string | undefined): string {
  if (!sha) return ''
  return sha.substring(0, 8)
}