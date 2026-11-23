import React, { useState } from 'react'
import { useDispatch } from 'react-redux'
import { useTranslate } from 'react-admin'
import {
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Typography,
} from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import GraphicEqIcon from '@material-ui/icons/GraphicEq'
import { NowPlayingToolButton } from '../nowPlaying/NowPlayingToolbar'
import { showNowPlayingTab } from '../nowPlaying/nowPlayingState'
import ToolSheet from '../playbackTools/ToolSheet'
import { useAudioEffectsAvailable } from '../audioplayer/audioEffects'
import { setVisuals } from '../actions'
import { OFF, VISUAL_MODES } from './visualModes'
import { useVisualShown, useVisuals } from './visualsSettings'
import { useRadioWaits } from './useVisualAnalyser'

const useStyles = makeStyles(
  (theme) => ({
    note: { padding: theme.spacing(0, 2.5, 1) },
    unavailable: {
      display: 'flex',
      flexDirection: 'column',
      alignItems: 'flex-start',
      gap: theme.spacing(1.5),
      padding: theme.spacing(0, 2.5, 1.5),
    },
  }),
  { name: 'NDVisualsTool' },
)

// PhotosensitivityWarning asks before visuals are turned on the first time.
// It is shown for as long as it is mounted.
const PhotosensitivityWarning = ({ onCancel, onAgree }) => {
  const translate = useTranslate()
  return (
    <Dialog
      open
      onClose={onCancel}
      aria-labelledby="visuals-warning-title"
      aria-describedby="visuals-warning-text"
    >
      <DialogTitle id="visuals-warning-title">
        {translate('nowPlaying.visuals.warningTitle')}
      </DialogTitle>
      <DialogContent>
        <DialogContentText id="visuals-warning-text">
          {translate('nowPlaying.visuals.warning')}
        </DialogContentText>
      </DialogContent>
      <DialogActions>
        <Button onClick={onCancel}>
          {translate('nowPlaying.visuals.cancel')}
        </Button>
        <Button onClick={onAgree} color="primary" variant="contained">
          {translate('nowPlaying.visuals.turnOn')}
        </Button>
      </DialogActions>
    </Dialog>
  )
}

// VisualsTool is the toolbar's Visuals: it shows the visual playing, and
// opens a sheet to choose one, or Off for the album art. The first time a
// visual is turned on it asks about photosensitivity first. It is left out
// while visuals are turned off in Personal; on an iPhone with Audio effects
// off its sheet points to that switch (ADR-0010).
const VisualsTool = ({ onNavigate }) => {
  const classes = useStyles()
  const translate = useTranslate()
  const dispatch = useDispatch()
  const { mode, warned, enabled } = useVisuals()
  const shown = useVisualShown()
  const available = useAudioEffectsAvailable()
  const radioWaits = useRadioWaits()
  const [open, setOpen] = useState(false)
  const [asking, setAsking] = useState(null)

  if (!enabled) return null

  const turnOn = (id, changes = {}) => {
    dispatch(setVisuals({ mode: id, ...changes }))
    dispatch(showNowPlayingTab('art'))
  }
  const choose = (id) => {
    if (id === OFF) dispatch(setVisuals({ mode: OFF }))
    else if (!warned) setAsking(id)
    else turnOn(id)
  }
  const modeLabel = (id) => translate(`nowPlaying.visuals.modes.${id}`)

  return (
    <>
      <NowPlayingToolButton
        icon={<GraphicEqIcon />}
        label={translate('nowPlaying.visuals.label')}
        value={shown ? modeLabel(shown) : undefined}
        active={!!shown}
        aria-haspopup="true"
        onClick={() => setOpen(true)}
      />
      <ToolSheet
        open={open}
        onClose={() => setOpen(false)}
        title={translate('nowPlaying.visuals.title')}
        choices={[OFF, ...VISUAL_MODES.map(({ id }) => id)].map((id) => ({
          value: id,
          label: modeLabel(id),
          disabled: !available && id !== OFF,
        }))}
        selected={available ? mode : OFF}
        onChoose={choose}
      >
        {!available ? (
          <div className={classes.unavailable}>
            <Typography variant="body2" color="textSecondary">
              {translate('nowPlaying.visuals.needsAudioEffects')}
            </Typography>
            <Button
              variant="outlined"
              color="primary"
              onClick={() => {
                setOpen(false)
                onNavigate('/personal')
              }}
            >
              {translate('nowPlaying.visuals.openSettings')}
            </Button>
          </div>
        ) : (
          radioWaits && (
            <Typography
              variant="body2"
              color="textSecondary"
              className={classes.note}
            >
              {translate('nowPlaying.visuals.radioWaits')}
            </Typography>
          )
        )}
      </ToolSheet>
      {asking && (
        <PhotosensitivityWarning
          onCancel={() => setAsking(null)}
          onAgree={() => {
            turnOn(asking, { warned: true })
            setAsking(null)
          }}
        />
      )}
    </>
  )
}

export default VisualsTool
