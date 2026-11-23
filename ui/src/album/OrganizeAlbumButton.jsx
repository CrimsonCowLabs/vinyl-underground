import React from 'react'
import PropTypes from 'prop-types'
import { usePermissions, useRefresh } from 'react-admin'
import { useLibraryEditing } from '../library/useLibraryEditing'
import OrganizeButton from '../organize/OrganizeButton'

const OrganizeAlbum = ({ record, size }) => {
  const refresh = useRefresh()
  return (
    <OrganizeButton
      scope={{ albumId: record.id }}
      label="resources.album.actions.organize"
      size={size}
      onDone={refresh}
    />
  )
}

OrganizeAlbum.propTypes = {
  record: PropTypes.object.isRequired,
  size: PropTypes.string,
}

// "Organize" on an album page (admin only, while the album's library is writable: ADR-0011):
// shows the plan that moves the album's Files to match their tags, and runs it once asked.
export const OrganizeAlbumButton = ({ record, size }) => {
  const { permissions } = usePermissions()
  const { isWritable } = useLibraryEditing()
  if (
    permissions !== 'admin' ||
    !record?.id ||
    isWritable(record.libraryId) !== true
  )
    return null
  return <OrganizeAlbum record={record} size={size} />
}

OrganizeAlbumButton.propTypes = {
  record: PropTypes.object,
  size: PropTypes.oneOf(['small', 'medium']),
}

export default OrganizeAlbumButton
