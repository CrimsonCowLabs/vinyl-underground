import { REST_URL } from '../../consts'

// jobUrl is a Rip job's URL in the Rip API: /tags to review it, DELETE to Discard it.
export const jobUrl = (jobId) => `${REST_URL}/rip/${jobId}`

export const errorText = (error) =>
  error?.body?.error || error?.message || 'ra.page.error'
