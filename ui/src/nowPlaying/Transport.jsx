import React, { useState } from 'react'
import { useTranslate } from 'react-admin'
import { IconButton, Slider } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import PlayArrowIcon from '@material-ui/icons/PlayArrow'
import PauseIcon from '@material-ui/icons/Pause'
import SkipNextIcon from '@material-ui/icons/SkipNext'
import SkipPreviousIcon from '@material-ui/icons/SkipPrevious'
import { vuPalette } from './vuPalette'

const useStyles = makeStyles(
  (theme) => {
    const vu = vuPalette(theme)
    return {
      seek: { padding: '0 2px' },
      slider: {
        display: 'block',
        padding: '8px 0',
        color: theme.palette.primary.main,
      },
      rail: {
        height: 4,
        borderRadius: 2,
        opacity: 1,
        backgroundColor: vu.surface3,
      },
      track: { height: 4, borderRadius: 2 },
      thumb: {
        width: 14,
        height: 14,
        marginTop: -5,
        marginLeft: -7,
        backgroundColor: theme.palette.text.primary,
      },
      times: {
        display: 'flex',
        justifyContent: 'space-between',
        fontSize: 12,
        color: theme.palette.text.secondary,
        fontVariantNumeric: 'tabular-nums',
      },
      controls: {
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        gap: theme.spacing(4),
      },
      skip: {
        color: theme.palette.text.primary,
        '& svg': { width: 34, height: 34 },
      },
      play: {
        width: 68,
        height: 68,
        backgroundColor: theme.palette.primary.main,
        color: theme.palette.primary.contrastText,
        boxShadow: `0 8px 22px ${vu.accentGlow}`,
        '&:hover': { backgroundColor: theme.palette.primary.main },
        '& svg': { width: 36, height: 36 },
      },
    }
  },
  { name: 'NDNowPlayingTransport' },
)

// formatTime shows seconds as m:ss (or h:mm:ss)
const formatTime = (seconds) => {
  const s = Math.max(0, Math.floor(seconds || 0))
  const h = Math.floor(s / 3600)
  const m = Math.floor(s / 60) % 60
  const ss = String(s % 60).padStart(2, '0')
  return h ? `${h}:${String(m).padStart(2, '0')}:${ss}` : `${m}:${ss}`
}

// Transport is the seek bar, with the time played and the time left, and
// previous, play/pause and next. A stream (Internet Radio) has no seek bar.
const Transport = ({ playback, seekable = true }) => {
  const classes = useStyles()
  const translate = useTranslate()
  // While the thumb is dragged it shows the drag, not playback
  const [dragging, setDragging] = useState(null)
  const { playing, currentTime, duration } = playback
  const position = dragging ?? Math.min(currentTime, duration || currentTime)

  return (
    <>
      {seekable && (
        <div className={classes.seek}>
          <Slider
            className={classes.slider}
            classes={{
              rail: classes.rail,
              track: classes.track,
              thumb: classes.thumb,
            }}
            min={0}
            max={Math.max(duration, 1)}
            step={1}
            value={position}
            aria-label={translate('nowPlaying.seek')}
            getAriaValueText={(v) => formatTime(v)}
            onChange={(e, v) => setDragging(v)}
            onChangeCommitted={(e, v) => {
              setDragging(null)
              playback.seek(v)
            }}
          />
          <div className={classes.times}>
            <span>{formatTime(position)}</span>
            <span>-{formatTime(duration - position)}</span>
          </div>
        </div>
      )}
      <div className={classes.controls}>
        <IconButton
          className={classes.skip}
          aria-label={translate('nowPlaying.previous')}
          onClick={playback.previous}
        >
          <SkipPreviousIcon />
        </IconButton>
        <IconButton
          className={classes.play}
          aria-label={translate(
            playing ? 'nowPlaying.pause' : 'nowPlaying.play',
          )}
          onClick={playback.togglePlay}
        >
          {playing ? <PauseIcon /> : <PlayArrowIcon />}
        </IconButton>
        <IconButton
          className={classes.skip}
          aria-label={translate('nowPlaying.next')}
          onClick={playback.next}
        >
          <SkipNextIcon />
        </IconButton>
      </div>
    </>
  )
}

export default Transport
