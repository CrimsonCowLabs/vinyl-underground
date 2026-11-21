import React, { useCallback, useEffect, useRef, useState } from 'react'
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
  LinearProgress,
  List,
  ListItem,
  ListItemText,
  TextField,
  Typography,
  useMediaQuery,
} from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import AutorenewIcon from '@material-ui/icons/Autorenew'
import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'
import config from '../config'

// Generated Playlists (ADR-0004): the server's LLM picks real Tracks from the
// Library for a Prompt. Keep these in sync with core/generatedplaylist.
export const DEFAULT_LENGTH = 25
export const MIN_LENGTH = 10
export const MAX_LENGTH = 100
const MAX_PROMPT_LENGTH = 500

const errorMessages = {
  aiUnavailable: 'resources.playlist.message.aiUnavailable',
  aiInvalidReply: 'resources.playlist.message.aiInvalidReply',
  noTracks: 'resources.playlist.message.noTracks',
}

const errorMessage = (err) =>
  errorMessages[err?.body?.error] || 'resources.playlist.message.generateFailed'

const useStyles = makeStyles((theme) => ({
  fields: {
    display: 'flex',
    gap: theme.spacing(2),
    alignItems: 'flex-start',
    flexWrap: 'wrap',
  },
  prompt: { flex: '1 1 16em' },
  length: { width: '8em' },
  progress: { margin: theme.spacing(3, 0, 1) },
  tracks: { maxHeight: '45vh', overflowY: 'auto' },
}))

const useElapsedSeconds = (running) => {
  const [seconds, setSeconds] = useState(0)
  useEffect(() => {
    if (!running) return undefined
    setSeconds(0)
    const started = Date.now()
    const timer = setInterval(
      () => setSeconds(Math.floor((Date.now() - started) / 1000)),
      1000,
    )
    return () => clearInterval(timer)
  }, [running])
  return seconds
}

const validLength = (value) => {
  const n = Number(value)
  return Number.isInteger(n) && n >= MIN_LENGTH && n <= MAX_LENGTH
}

const GeneratePlaylistDialog = ({ open, onClose }) => {
  const translate = useTranslate()
  const notify = useNotify()
  const redirect = useRedirect()
  const refresh = useRefresh()
  const classes = useStyles()
  const fullScreen = useMediaQuery((theme) => theme.breakpoints.down('xs'))

  const [prompt, setPrompt] = useState('')
  const [length, setLength] = useState(String(DEFAULT_LENGTH))
  const [generating, setGenerating] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState(null)
  const [preview, setPreview] = useState(null)
  const [name, setName] = useState('')
  const elapsed = useElapsedSeconds(generating)
  // Ignore a reply that arrives after the dialog was closed or a newer request started.
  const request = useRef(0)

  useEffect(() => {
    if (!open) request.current++
  }, [open])

  const canGenerate =
    !generating &&
    prompt.trim() !== '' &&
    prompt.length <= MAX_PROMPT_LENGTH &&
    validLength(length)

  const handleGenerate = useCallback(
    (e) => {
      e?.preventDefault()
      if (!canGenerate) return
      const id = ++request.current
      setGenerating(true)
      setError(null)
      setPreview(null)
      httpClient(`${REST_URL}/generatedPlaylist/preview`, {
        method: 'POST',
        body: JSON.stringify({ prompt: prompt.trim(), length: Number(length) }),
      })
        .then(({ json }) => {
          if (id !== request.current) return
          setPreview(json)
          setName(json.name || '')
        })
        .catch((err) => {
          if (id !== request.current) return
          setError(errorMessage(err))
        })
        .finally(() => {
          if (id === request.current) setGenerating(false)
        })
    },
    [canGenerate, prompt, length],
  )

  const handleSave = useCallback(() => {
    setSaving(true)
    httpClient(`${REST_URL}/generatedPlaylist`, {
      method: 'POST',
      body: JSON.stringify({
        name: name.trim(),
        prompt: preview.prompt,
        trackIds: preview.tracks.map((t) => t.id),
      }),
    })
      .then(({ json }) => {
        notify('resources.playlist.message.generatedSaved', { type: 'info' })
        refresh()
        onClose()
        redirect(`/playlist/${json.id}/show`)
      })
      .catch((err) => setError(errorMessage(err)))
      .finally(() => setSaving(false))
  }, [name, preview, notify, refresh, onClose, redirect])

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

        {generating && (
          <div className={classes.progress}>
            <LinearProgress />
            <Typography variant="body2" color="textSecondary">
              {translate('resources.playlist.message.generating', {
                seconds: elapsed,
              })}
            </Typography>
          </div>
        )}

        {error && (
          <Typography color="error" role="alert">
            {translate(error)}
          </Typography>
        )}

        {preview && !generating && (
          <>
            <TextField
              id="generate-playlist-name"
              label={translate('resources.playlist.fields.name')}
              value={name}
              onChange={(e) => setName(e.target.value)}
              margin="normal"
              fullWidth
            />
            <Typography variant="body2" color="textSecondary">
              {translate('resources.song.name', {
                smart_count: preview.tracks.length,
              })}
            </Typography>
            <List dense className={classes.tracks}>
              {preview.tracks.map((t) => (
                <ListItem key={t.id} disableGutters>
                  <ListItemText
                    primary={t.title}
                    secondary={[t.artist, t.year || null]
                      .filter(Boolean)
                      .join(' · ')}
                    primaryTypographyProps={{ noWrap: true }}
                    secondaryTypographyProps={{ noWrap: true }}
                  />
                </ListItem>
              ))}
            </List>
          </>
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
