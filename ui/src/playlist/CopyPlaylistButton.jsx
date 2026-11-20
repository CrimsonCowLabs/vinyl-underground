import React, { useCallback, useState } from 'react'
import PropTypes from 'prop-types'
import {
  Button,
  useNotify,
  useRedirect,
  useRefresh,
  useTranslate,
} from 'react-admin'
import FileCopyOutlinedIcon from '@material-ui/icons/FileCopyOutlined'
import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'
import { isWritable } from '../common/playlistUtils'

// "Copy to my Playlists": offered on a Playlist shared by another Listener, which
// is read-only to you. Creates your own private, editable copy and opens it.
export const CopyPlaylistButton = ({ record }) => {
  const translate = useTranslate()
  const notify = useNotify()
  const redirect = useRedirect()
  const refresh = useRefresh()
  const [copying, setCopying] = useState(false)

  const handleCopy = useCallback(() => {
    setCopying(true)
    httpClient(`${REST_URL}/playlist/${record.id}/copy`, { method: 'POST' })
      .then((res) => {
        notify('resources.playlist.message.copied', { type: 'info' })
        refresh()
        redirect(`/playlist/${res.json.id}/show`)
      })
      .catch(() => notify('ra.page.error', { type: 'warning' }))
      .finally(() => setCopying(false))
  }, [record, notify, redirect, refresh])

  if (!record?.id || isWritable(record.ownerId)) {
    return null
  }

  return (
    <Button
      onClick={handleCopy}
      disabled={copying}
      label={translate('resources.playlist.actions.copyToMyPlaylists')}
    >
      <FileCopyOutlinedIcon />
    </Button>
  )
}

CopyPlaylistButton.propTypes = {
  record: PropTypes.object.isRequired,
}
