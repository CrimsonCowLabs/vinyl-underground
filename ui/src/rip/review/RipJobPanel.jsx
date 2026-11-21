import React from 'react'
import ReviewCard from './ReviewCard'
import { DiscardJobButton } from './DiscardDialog'
import { Typography } from '@material-ui/core'
import JobOrigin from '../JobOrigin'

// RipJobPanel is a job's expanded row in the Rip list: the review card once the job is ready
// (including ready with failures), and the fallback (the item list) while it runs. A failed job
// has no Files to review, but can still be discarded, which also removes its empty folder. The
// card says where its Files came from: "Uploaded", "Dropped into the Inbox" (#35) or the Rip's
// URL.
const RipJobPanel = ({ record, fallback, trackExtra, actions, ...rest }) => {
  if (record?.status === 'ready') {
    return (
      <>
        <Typography variant="subtitle2" gutterBottom data-testid="job-origin">
          <JobOrigin record={record} />
        </Typography>
        <ReviewCard record={record} trackExtra={trackExtra} actions={actions} />
      </>
    )
  }
  const items = fallback
    ? React.cloneElement(fallback, { record, trackExtra, ...rest })
    : null
  if (record?.status !== 'failed') return items
  return (
    <>
      {items}
      <DiscardJobButton
        jobId={record.id}
        fileCount={(record.items || []).filter((it) => it.path).length}
      />
    </>
  )
}

export default RipJobPanel
