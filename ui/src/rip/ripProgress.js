// ripProgress counts a Rip job's Tracks: those in the Inbox (ready), those that failed, and how
// many are done either way, out of the total.
export const ripProgress = (job) => {
  const items = job?.items || []
  const ready = items.filter((it) => it.status === 'ready').length
  const failed = items.filter((it) => it.status === 'failed').length
  return { total: items.length, ready, failed, done: ready + failed }
}
