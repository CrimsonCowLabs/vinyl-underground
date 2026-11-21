import React, { useCallback, useEffect, useState } from 'react'
import PropTypes from 'prop-types'
import {
  Button,
  useNotify,
  useRedirect,
  useRefresh,
  useTranslate,
} from 'react-admin'
import {
  Button as MuiButton,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Typography,
  useMediaQuery,
} from '@material-ui/core'
import AutorenewIcon from '@material-ui/icons/Autorenew'
import config from '../config'
import { httpClient } from '../dataProvider'
import { isWritable } from '../common/playlistUtils'
import {
  GENERATED_PLAYLIST_URL,
  errorMessage,
  saveGeneratedPlaylist,
  usePreview,
} from './generatedPlaylist'
import { PreviewStatus, PreviewTracks } from './GeneratedPlaylistPreview'

const RegeneratePlaylistDialog = ({ record, onClose }) => {
  const translate = useTranslate()
  const notify = useNotify()
  const redirect = useRedirect()
  const refresh = useRefresh()
  const fullScreen = useMediaQuery((theme) => theme.breakpoints.down('xs'))
  const [saving, setSaving] = useState(false)
  const { generating, error, setError, preview, generate } = usePreview()
  const canReplace = isWritable(record.ownerId)

  // The server reruns the stored Prompt at its stored length
  const handleRegenerate = useCallback(
    () => generate(`${GENERATED_PLAYLIST_URL}/${record.id}/preview`),
    [generate, record.id],
  )
  useEffect(() => {
    handleRegenerate()
  }, [handleRegenerate])

  const handleReplace = useCallback(() => {
    setSaving(true)
    httpClient(`${GENERATED_PLAYLIST_URL}/${record.id}/tracks`, {
      method: 'PUT',
      body: JSON.stringify({ trackIds: preview.tracks.map((t) => t.id) }),
    })
      .then(() => {
        notify('resources.playlist.message.generatedReplaced', { type: 'info' })
        refresh()
        onClose()
      })
      .catch((err) => {
        setError(errorMessage(err))
        setSaving(false)
      })
  }, [record.id, preview, notify, refresh, onClose, setError])

  const handleSaveAsNew = useCallback(() => {
    setSaving(true)
    saveGeneratedPlaylist(preview, preview.name || '')
      .then((id) => {
        notify('resources.playlist.message.generatedSaved', { type: 'info' })
        refresh()
        onClose()
        redirect(`/playlist/${id}/show`)
      })
      .catch((err) => {
        setError(errorMessage(err))
        setSaving(false)
      })
  }, [preview, notify, refresh, onClose, redirect, setError])

  const ready = preview && !generating && preview.tracks.length > 0

  return (
    <Dialog
      open
      onClose={onClose}
      fullWidth
      maxWidth="sm"
      fullScreen={fullScreen}
      aria-labelledby="regenerate-playlist-title"
    >
      <DialogTitle id="regenerate-playlist-title">
        {translate('resources.playlist.actions.regeneratePlaylist')}
      </DialogTitle>
      <DialogContent>
        <Typography variant="body2" color="textSecondary">
          {translate('resources.playlist.message.generatedFrom', {
            prompt: record.prompt,
          })}
        </Typography>
        <PreviewStatus generating={generating} error={error} />
        {preview && !generating && <PreviewTracks preview={preview} />}
      </DialogContent>
      <DialogActions>
        <MuiButton onClick={onClose}>{translate('ra.action.cancel')}</MuiButton>
        <MuiButton onClick={handleRegenerate} disabled={generating || saving}>
          {translate('resources.playlist.actions.regenerate')}
        </MuiButton>
        {ready && (
          <MuiButton
            onClick={handleSaveAsNew}
            disabled={saving}
            color="primary"
          >
            {translate('resources.playlist.actions.saveAsNew')}
          </MuiButton>
        )}
        {ready && canReplace && (
          <MuiButton
            onClick={handleReplace}
            disabled={saving}
            color="primary"
            variant="contained"
          >
            {translate('resources.playlist.actions.replaceTracks')}
          </MuiButton>
        )}
      </DialogActions>
    </Dialog>
  )
}

// "Regenerate": offered on a Generated Playlist when the server has an LLM.
// Reruns its Prompt for a fresh preview, which can replace the Playlist's
// Tracks (if it's the Listener's own) or be saved as a new Playlist.
export const RegeneratePlaylistButton = ({ record }) => {
  const translate = useTranslate()
  const [open, setOpen] = useState(false)
  const handleClose = useCallback(() => setOpen(false), [])

  if (!config.enableGeneratedPlaylists || !record?.id || !record.prompt) {
    return null
  }

  return (
    <>
      <Button
        onClick={() => setOpen(true)}
        label={translate('resources.playlist.actions.regeneratePlaylist')}
      >
        <AutorenewIcon />
      </Button>
      {open && (
        <RegeneratePlaylistDialog record={record} onClose={handleClose} />
      )}
    </>
  )
}

RegeneratePlaylistButton.propTypes = {
  record: PropTypes.object,
}

export default RegeneratePlaylistButton
