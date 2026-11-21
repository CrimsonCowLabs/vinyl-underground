import React, { useEffect, useState } from 'react'
import { useTranslate } from 'react-admin'
import {
  LinearProgress,
  List,
  ListItem,
  ListItemText,
  TextField,
  Typography,
} from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'

// The preview parts shared by the Generate and Regenerate dialogs.

const useStyles = makeStyles((theme) => ({
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

// PreviewStatus shows the progress while the LLM works, or what went wrong.
export const PreviewStatus = ({ generating, error }) => {
  const translate = useTranslate()
  const classes = useStyles()
  const elapsed = useElapsedSeconds(generating)
  return (
    <>
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
    </>
  )
}

// PreviewTracks lists a preview's Tracks, under an editable name when
// onNameChange is given.
export const PreviewTracks = ({ id, preview, name, onNameChange }) => {
  const translate = useTranslate()
  const classes = useStyles()
  return (
    <>
      {onNameChange && (
        <TextField
          id={id}
          label={translate('resources.playlist.fields.name')}
          value={name}
          onChange={(e) => onNameChange(e.target.value)}
          margin="normal"
          fullWidth
        />
      )}
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
              secondary={[t.artist, t.year || null].filter(Boolean).join(' · ')}
              primaryTypographyProps={{ noWrap: true }}
              secondaryTypographyProps={{ noWrap: true }}
            />
          </ListItem>
        ))}
      </List>
    </>
  )
}
