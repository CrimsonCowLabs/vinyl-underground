import React from 'react'
import { useTranslate } from 'react-admin'
import { Link } from '@material-ui/core'

// JobOrigin says where a job's Files came from: "Uploaded", "Dropped into the Inbox" (#35), or a
// Rip's URL, as a link.
const JobOrigin = ({ record }) => {
  const translate = useTranslate()
  if (!record) return null
  if (record.kind === 'upload' || record.kind === 'drop') {
    return <span>{translate(`resources.rip.kind.${record.kind}`)}</span>
  }
  if (!record.url) return null
  return (
    <Link
      href={record.url}
      target="_blank"
      rel="noopener noreferrer"
      onClick={(e) => e.stopPropagation()}
    >
      {record.url}
    </Link>
  )
}

export default JobOrigin
