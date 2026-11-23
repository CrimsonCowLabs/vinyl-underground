import React, { useState } from 'react'
import PropTypes from 'prop-types'
import { Button, useRedirect, useRefresh, useTranslate } from 'react-admin'
import { MdDeleteSweep } from 'react-icons/md'
import { useLibraryEditing } from '../library/useLibraryEditing'
import { SendToQuarantineDialog } from './SendToQuarantineDialog'

// SendAlbumToQuarantineButton sends every Track of the album to Quarantine, as one job (#168):
// for the admin, while the album's library is writable. The album leaves the Library, so the
// page goes back to the album list once every Track has.
export const SendAlbumToQuarantineButton = ({ record, size }) => {
  const { enabled, isWritable } = useLibraryEditing()
  if (!record?.id || !enabled || isWritable(record.libraryId) !== true) {
    return null
  }
  return <SendAlbumButton record={record} size={size} />
}

SendAlbumToQuarantineButton.propTypes = {
  record: PropTypes.object,
  size: PropTypes.string,
}

const SendAlbumButton = ({ record, size }) => {
  const translate = useTranslate()
  const redirect = useRedirect()
  const refresh = useRefresh()
  const [open, setOpen] = useState(false)
  return (
    <>
      <Button
        onClick={() => setOpen(true)}
        label={translate('resources.quarantine.actions.sendAlbum')}
        size={size}
      >
        <MdDeleteSweep />
      </Button>
      {open && (
        <SendToQuarantineDialog
          open
          selection={{ albumId: record.id }}
          count={record.songCount}
          onClose={() => setOpen(false)}
          onDone={({ job, background }) => {
            setOpen(false)
            // Gone from the Library once every Track went; otherwise it is still here.
            if (!background && !job?.skipped) redirect('/album')
            else refresh()
          }}
        />
      )}
    </>
  )
}

SendAlbumButton.propTypes = {
  record: PropTypes.object.isRequired,
  size: PropTypes.string,
}

export default SendAlbumToQuarantineButton
