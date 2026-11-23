import React from 'react'
import ReviewCard from './ReviewCard'
import { DiscardJobButton } from './DiscardDialog'
import { Typography } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import JobOrigin from '../JobOrigin'
import { Clipped } from '../../common/Clipped'

const useStyles = makeStyles(
  {
    // The review card takes the width of the job's row and fits itself into it, rather than its
    // fields and URL asking the job table for more room. With no width of its own (width: 0), it
    // can't widen the table; min-width: 100% then fills the row
    fit: {
      width: 0,
      minWidth: '100%',
      // On a phone (no side drawer or Now Playing panel, as in Layout) the job table is wider
      // than the screen and scrolls sideways (see RipList), so the card keeps to the screen's
      // width, and stays in view as the table scrolls
      '@media (max-width: 809.95px)': {
        position: 'sticky',
        left: 16,
        width: 'calc(100vw - 32px)',
        minWidth: 0,
      },
    },
  },
  { name: 'NDRipJobPanel' },
)

// RipJobPanel is a job's expanded row in the Rip list: the review card once the job is ready
// (including ready with failures), and the fallback (the item list) while it runs. A failed job
// has no Files to review, but can still be discarded, which also removes its empty folder. The
// card says where its Files came from: "Uploaded", "Dropped into the Inbox" (#35) or the Rip's
// URL.
const RipJobPanel = ({ record, fallback, trackExtra, actions, ...rest }) => {
  const classes = useStyles()
  if (record?.status === 'ready') {
    return (
      <div className={classes.fit}>
        <Typography
          variant="subtitle2"
          component="div"
          gutterBottom
          data-testid="job-origin"
        >
          {/* On one line, so a long URL can't widen the job table */}
          <Clipped title={record.url}>
            <JobOrigin record={record} />
          </Clipped>
        </Typography>
        <ReviewCard record={record} trackExtra={trackExtra} actions={actions} />
      </div>
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
