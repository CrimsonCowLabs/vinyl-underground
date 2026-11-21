import React, { useState } from 'react'
import PropTypes from 'prop-types'
import { Button, Confirm, useNotify, useTranslate } from 'react-admin'
import { CircularProgress } from '@material-ui/core'
import GetAppIcon from '@material-ui/icons/GetApp'
import OfflinePinIcon from '@material-ui/icons/OfflinePin'
import ReplayIcon from '@material-ui/icons/Replay'
import { downloads, downloadsSupported, useDownload } from '../download'

// Stores a Playlist on the device for playing offline (a Download), shows its
// progress, and removes it again.
export const DownloadPlaylistButton = ({ record }) => {
  const translate = useTranslate()
  const notify = useNotify()
  const download = useDownload(record.id)
  const [confirming, setConfirming] = useState(false)

  if (!downloadsSupported()) return null

  const start = () =>
    downloads
      .start(record.id)
      .catch(() =>
        notify('resources.playlist.message.downloadFailed', 'warning'),
      )

  const remove = () => {
    setConfirming(false)
    downloads
      .remove(record.id)
      .then(() => notify('resources.playlist.message.downloadRemoved', 'info'))
      .catch(() =>
        notify('resources.playlist.message.removeDownloadFailed', 'warning'),
      )
  }

  let label, icon, onClick
  switch (download?.state) {
    case 'downloading':
      label = translate('resources.playlist.message.downloading', download)
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
      label = translate('resources.playlist.actions.resumeDownload', download)
      icon = <ReplayIcon />
      onClick = () => downloads.resume()
      break
    case 'downloaded':
      label = translate('resources.playlist.message.downloaded')
      icon = <OfflinePinIcon />
      onClick = () => setConfirming(true)
      break
    default:
      label = translate('resources.playlist.actions.download')
      icon = <GetAppIcon />
      onClick = start
  }

  return (
    <>
      <Button onClick={onClick} label={label}>
        {icon}
      </Button>
      <Confirm
        isOpen={confirming}
        title="resources.playlist.message.removeDownloadTitle"
        content="resources.playlist.message.removeDownloadContent"
        translateOptions={{ name: record.name }}
        confirm="resources.playlist.actions.removeDownload"
        onConfirm={remove}
        onClose={() => setConfirming(false)}
      />
    </>
  )
}

DownloadPlaylistButton.propTypes = {
  record: PropTypes.object.isRequired,
}
