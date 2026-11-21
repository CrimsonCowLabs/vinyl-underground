import { useListContext, useRefresh } from 'react-admin'
import { useInterval } from '../common/useInterval'

// Jobs run on the server whether or not the page is open; while it is, it polls for their status.
const POLL_INTERVAL_MS = 3000

const ACTIVE = ['queued', 'downloading']

export const hasActiveRips = (data) =>
  Object.values(data || {}).some((job) => ACTIVE.includes(job?.status))

// useRipPolling refreshes the job list while any Rip is still queued or downloading.
export const useRipPolling = () => {
  const { data } = useListContext()
  const refresh = useRefresh()
  useInterval(refresh, hasActiveRips(data) ? POLL_INTERVAL_MS : null)
}
