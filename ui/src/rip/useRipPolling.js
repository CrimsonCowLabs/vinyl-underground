import { useListContext, useRefresh } from 'react-admin'
import { useInterval } from '../common/useInterval'

// Jobs run on the server whether or not the page is open; while it is, it polls for their status.
const POLL_INTERVAL_MS = 3000
// IDLE_POLL_MS is how often the list looks again with nothing running, for the cards of Files
// dropped into the Inbox (#35), which appear after a scan.
export const IDLE_POLL_MS = 15000

const ACTIVE = ['queued', 'downloading']

export const hasActiveRips = (data) =>
  Object.values(data || {}).some((job) => ACTIVE.includes(job?.status))

// useRipPolling refreshes the job list often while any Rip is still queued or downloading, and
// now and then otherwise.
export const useRipPolling = () => {
  const { data } = useListContext()
  const refresh = useRefresh()
  useInterval(refresh, hasActiveRips(data) ? POLL_INTERVAL_MS : IDLE_POLL_MS)
}
