import React from 'react'
import { BooleanField } from 'react-admin'

// LibraryEditingField shows whether the app can edit a library's Files: its folder was writable
// at startup (ADR-0011). It is blank for a library that wasn't checked (the Inbox, or one added
// since startup) and while the answer is loading.
const LibraryEditingField = ({ record, isWritable, source, ...rest }) => {
  const writable = record ? isWritable(record.id) : undefined
  if (writable === undefined) return null
  return (
    <BooleanField
      {...rest}
      source={source}
      record={{ ...record, [source]: writable }}
    />
  )
}

export default LibraryEditingField
