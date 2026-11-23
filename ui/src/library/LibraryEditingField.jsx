import React from 'react'
import { BooleanField } from 'react-admin'

// LibraryEditingField shows whether the app can edit a library's Files: its folder was writable
// at startup (ADR-0011).
const LibraryEditingField = ({ record, isWritable, source, ...rest }) => (
  <BooleanField
    {...rest}
    source={source}
    record={{ ...record, [source]: !!record && isWritable(record.id) }}
  />
)

export default LibraryEditingField
