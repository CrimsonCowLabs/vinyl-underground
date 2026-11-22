import React, { useCallback, useState } from 'react'
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
  TextField,
  useMediaQuery,
} from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import AutorenewIcon from '@material-ui/icons/Autorenew'
import config from '../config'
import {
  DEFAULT_LENGTH,
  GENERATED_PLAYLIST_URL,
  MAX_LENGTH,
  MAX_PROMPT_LENGTH,
  MIN_LENGTH,
  errorMessage,
  saveGeneratedPlaylist,
  usePreview,
} from './generatedPlaylist'
import { PreviewStatus, PreviewTracks } from './GeneratedPlaylistPreview'

const useStyles = makeStyles((theme) => ({
  fields: {
    display: 'flex',
    gap: theme.spacing(2),
    alignItems: 'flex-start',
    flexWrap: 'wrap',
  },
  prompt: { flex: '1 1 16em' },
  length: { width: '8em' },
}))

const validLength = (value) => {
  const n = Number(value)
  return Number.isInteger(n) && n >= MIN_LENGTH && n <= MAX_LENGTH
}

export const GeneratePlaylistDialog = ({ open, onClose }) => {
  const translate = useTranslate()
  const notify = useNotify()
  const redirect = useRedirect()
  const refresh = useRefresh()
  const classes = useStyles()
  const fullScreen = useMediaQuery((theme) => theme.breakpoints.down('xs'))

  const [prompt, setPrompt] = useState('')
  const [length, setLength] = useState(String(DEFAULT_LENGTH))
  const [saving, setSaving] = useState(false)
  const [name, setName] = useState('')
  const { generating, error, setError, preview, generate } = usePreview()

  const canGenerate =
    !generating &&
    prompt.trim() !== '' &&
    prompt.length <= MAX_PROMPT_LENGTH &&
    validLength(length)

  const handleGenerate = useCallback(
    (e) => {
      e?.preventDefault()
      if (!canGenerate) return
      generate(`${GENERATED_PLAYLIST_URL}/preview`, {
        prompt: prompt.trim(),
        length: Number(length),
      }).then((json) => json && setName(json.name || ''))
    },
    [canGenerate, generate, prompt, length],
  )

  const handleSave = useCallback(() => {
    setSaving(true)
    saveGeneratedPlaylist(preview, name)
      .then((id) => {
        notify('resources.playlist.message.generatedSaved', { type: 'info' })
        refresh()
        onClose()
        redirect(`/playlist/${id}/show`)
      })
      .catch((err) => setError(errorMessage(err)))
      .finally(() => setSaving(false))
  }, [name, preview, notify, refresh, onClose, redirect, setError])

  const lengthInvalid = !validLength(length)

  return (
    <Dialog
      open={open}
      onClose={onClose}
      fullWidth
      maxWidth="sm"
      fullScreen={fullScreen}
      aria-labelledby="generate-playlist-title"
    >
      <DialogTitle id="generate-playlist-title">
        {translate('resources.playlist.actions.generate')}
      </DialogTitle>
      <DialogContent>
        <form onSubmit={handleGenerate} className={classes.fields}>
          <TextField
            className={classes.prompt}
            id="generate-playlist-prompt"
            label={translate('resources.playlist.fields.prompt')}
            placeholder={translate('resources.playlist.message.promptHint')}
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            disabled={generating}
            inputProps={{ maxLength: MAX_PROMPT_LENGTH }}
            autoFocus
            fullWidth
          />
          <TextField
            className={classes.length}
            id="generate-playlist-length"
            label={translate('resources.playlist.fields.length')}
            type="number"
            value={length}
            onChange={(e) => setLength(e.target.value)}
            disabled={generating}
            error={lengthInvalid}
            helperText={
              lengthInvalid
                ? translate('resources.playlist.message.lengthRange', {
                    min: MIN_LENGTH,
                    max: MAX_LENGTH,
                  })
                : ' '
            }
            inputProps={{ min: MIN_LENGTH, max: MAX_LENGTH, step: 1 }}
          />
        </form>

        <PreviewStatus generating={generating} error={error} />

        {preview && !generating && (
          <PreviewTracks
            id="generate-playlist-name"
            preview={preview}
            name={name}
            onNameChange={setName}
          />
        )}
      </DialogContent>
      <DialogActions>
        <MuiButton onClick={onClose}>{translate('ra.action.cancel')}</MuiButton>
        <MuiButton
          onClick={handleGenerate}
          disabled={!canGenerate}
          color={preview ? 'default' : 'primary'}
        >
          {translate(
            preview
              ? 'resources.playlist.actions.regenerate'
              : 'resources.playlist.actions.generatePreview',
          )}
        </MuiButton>
        {preview && !generating && (
          <MuiButton
            onClick={handleSave}
            disabled={saving || preview.tracks.length === 0}
            color="primary"
            variant="contained"
          >
            {translate('ra.action.save')}
          </MuiButton>
        )}
      </DialogActions>
    </Dialog>
  )
}

// "Generate": offered in the Playlists list when the server has an LLM.
export const GeneratePlaylistButton = () => {
  const translate = useTranslate()
  const [open, setOpen] = useState(false)
  const handleClose = useCallback(() => setOpen(false), [])

  if (!config.enableGeneratedPlaylists) {
    return null
  }

  return (
    <>
      <Button
        onClick={() => setOpen(true)}
        label={translate('resources.playlist.actions.generate')}
      >
        <AutorenewIcon />
      </Button>
      {open && <GeneratePlaylistDialog open={open} onClose={handleClose} />}
    </>
  )
}

export default GeneratePlaylistButton
