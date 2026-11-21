import React from 'react'
import ReviewCard from './ReviewCard'

// The job's statuses whose Files have all landed, so it can be reviewed.
const REVIEWABLE = ['ready']

// RipJobPanel is a job's expanded row in the Rip list: the review card once the job is ready
// (including ready with failures), and the fallback (the item list) while it runs or if it
// failed.
const RipJobPanel = ({ record, fallback, ...rest }) => {
  if (record && REVIEWABLE.includes(record.status)) {
    return <ReviewCard record={record} {...rest} />
  }
  return fallback ? React.cloneElement(fallback, { record, ...rest }) : null
}

export default RipJobPanel
