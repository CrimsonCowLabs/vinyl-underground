import React, { useState } from 'react'
import PropTypes from 'prop-types'
import { Button, Confirm, useNotify, useTranslate } from 'react-admin'
import { CircularProgress } from '@material-ui/core'
import GetAppIcon from '@material-ui/icons/GetApp'
import OfflinePinIcon from '@material-ui/icons/OfflinePin'
import ReplayIcon from '@material-ui/icons/Replay'
import { downloads, downloadsSupported, useDownload } from './index'

// Stores a Playlist or album on the device for playing offline (a Download),
// shows its progress, and removes it again.
export const DownloadButton = ({ record, kind }) => {
  const translate = useTranslate()
  const notify = useNotify()
  const download = useDownload(record.id, kind)
  const [confirming, setConfirming] = useState(false)

  if (!downloadsSupported()) return null

  const start = () =>
    downloads.start(record.id, kind).catch(() =>
      notify('download.message.failed', 'warning', {
        name: record.name,
      }),
    )

  const remove = () => {
    setConfirming(false)
    downloads
      .remove(record.id, kind)
      .then(() => notify('download.message.removed', 'info'))
      .catch(() => notify('download.message.removeFailed', 'warning'))
  }

  let label, icon, onClick
  switch (download?.state) {
    case 'downloading':
      label = translate('download.message.downloading', download)
      icon = (
        <CircularProgress
          size={18}
          variant="determinate"
          value={(100 * download.done) / Math.max(download.total, 1)}
        />
      )
      onClick = () => setConfirming(true)
      break
    case 'interrupted':
      label = translate('download.actions.resume', download)
      icon = <ReplayIcon />
      onClick = () => downloads.resume()
      break
    case 'downloaded':
      label = translate('download.message.downloaded')
      icon = <OfflinePinIcon />
      onClick = () => setConfirming(true)
      break
    default:
      label = translate('download.actions.download')
      icon = <GetAppIcon />
      onClick = start
  }

  return (
    <>
      <Button onClick={onClick} label={label} disabled={record.missing}>
        {icon}
      </Button>
      <Confirm
        isOpen={confirming}
        title="download.message.removeTitle"
        content="download.message.removeContent"
        translateOptions={{ name: record.name }}
        confirm="download.actions.remove"
        onConfirm={remove}
        onClose={() => setConfirming(false)}
      />
    </>
  )
}

DownloadButton.propTypes = {
  record: PropTypes.object.isRequired,
  kind: PropTypes.oneOf(['playlist', 'album']).isRequired,
}
