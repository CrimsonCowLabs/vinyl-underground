import React from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { useTranslate } from 'react-admin'
import { ButtonBase, IconButton, LinearProgress } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import PlayArrowIcon from '@material-ui/icons/PlayArrow'
import PauseIcon from '@material-ui/icons/Pause'
import SkipNextIcon from '@material-ui/icons/SkipNext'
import { useStationDJ } from '../nowPlaying/useStationDJ'
import { usePlayback } from '../nowPlaying/usePlayback'
import { openNowPlaying } from '../nowPlaying/nowPlayingState'
import { vuPalette } from '../nowPlaying/vuPalette'

const useStyles = makeStyles(
  (theme) => {
    const vu = vuPalette(theme)
    const line = {
      display: 'block',
      whiteSpace: 'nowrap',
      overflow: 'hidden',
      textOverflow: 'ellipsis',
    }
    return {
      root: {
        position: 'fixed',
        left: 8,
        right: 8,
        bottom: 'calc(env(safe-area-inset-bottom) + 8px)',
        zIndex: theme.zIndex.appBar,
        display: 'grid',
        gridTemplateColumns: 'minmax(0, 1fr) auto auto',
        columnGap: 4,
        alignItems: 'center',
        padding: '8px 6px 8px 8px',
        borderRadius: 12,
        // Surface 2 is opaque in the Vinyl Underground theme; the inherited
        // themes' stand-in is a tint, so it is laid over the paper
        backgroundColor: theme.palette.background.paper,
        backgroundImage: `linear-gradient(${vu.surface2}, ${vu.surface2})`,
        boxShadow: theme.shadows[8],
        color: theme.palette.text.primary,
        fontFamily: theme.typography.fontFamily,
        overflow: 'hidden',
        cursor: 'pointer',
      },
      open: {
        display: 'grid',
        gridTemplateColumns: '44px minmax(0, 1fr)',
        gap: 10,
        alignItems: 'center',
        justifyContent: 'stretch',
        textAlign: 'left',
        font: 'inherit',
        borderRadius: 6,
        minWidth: 0,
      },
      cover: {
        width: 44,
        height: 44,
        borderRadius: 6,
        display: 'block',
        objectFit: 'cover',
        backgroundColor: vu.surface3,
      },
      text: { minWidth: 0 },
      title: { ...line, fontSize: 14, fontWeight: 700 },
      sub: { ...line, fontSize: 12.5, color: theme.palette.text.secondary },
      dj: { color: theme.palette.primary.main, fontWeight: 700 },
      button: { color: theme.palette.text.primary, padding: 10 },
      progress: {
        position: 'absolute',
        left: 8,
        right: 8,
        bottom: 0,
        height: 2,
        backgroundColor: vu.surface3,
      },
      bar: {
        backgroundColor: theme.palette.primary.main,
        // Follows timeupdate, a few times a second, without lagging behind
        transition: 'none',
      },
    }
  },
  { name: 'NDMiniPlayer' },
)

// MINI_PLAYER_ROOM is the room the MiniPlayer takes along the bottom of the
// page: its 60px, the 8px below it and 8px above, over the home indicator
export const MINI_PLAYER_ROOM = 'calc(env(safe-area-inset-bottom) + 76px)'

// useQueued is the Track playing, or before anything has played since a
// reload, the one the queue would play
const useQueued = () =>
  useSelector(({ player }) =>
    player.current?.uuid
      ? player.current
      : player.queue[player.savedPlayIndex || 0],
  )

// MiniPlayer is the compact player along the bottom of a phone: the cover,
// title and artist (with the DJ's name on a Station), play/pause and next,
// and a thin progress line. Tapping it (anywhere but its buttons) opens Now
// Playing. It drives the player package's own audio element, so the package
// keeps doing playback and the queue.
const MiniPlayer = ({ audioInstance }) => {
  const classes = useStyles()
  const translate = useTranslate()
  const dispatch = useDispatch()
  const queued = useQueued()
  const dj = useStationDJ()
  const playback = usePlayback(audioInstance, queued?.duration)
  if (!queued) return null
  const song = queued.song || {}
  const { playing, currentTime, duration } = playback
  const percent = duration
    ? Math.min(100, Math.round((currentTime / duration) * 100))
    : 0
  // A tap anywhere opens Now Playing, except on play/pause and next
  const withoutOpening = (action) => (e) => {
    e.stopPropagation()
    action()
  }

  return (
    <div className={classes.root} onClick={() => dispatch(openNowPlaying())}>
      {/* The keyboard's and screen readers' way in; its click opens it too */}
      <ButtonBase
        className={classes.open}
        aria-label={translate('nowPlaying.open')}
      >
        {queued.cover ? (
          <img className={classes.cover} src={queued.cover} alt="" />
        ) : (
          <span className={classes.cover} />
        )}
        <span className={classes.text}>
          <span className={classes.title}>
            {queued.isRadio ? queued.name : song.title || queued.name}
          </span>
          <span className={classes.sub}>
            {dj?.name && (
              <>
                <span className={classes.dj}>{dj.name}</span>
                {' · '}
              </>
            )}
            {!queued.isRadio && <span>{song.artist}</span>}
          </span>
        </span>
      </ButtonBase>
      <IconButton
        className={classes.button}
        aria-label={translate(playing ? 'nowPlaying.pause' : 'nowPlaying.play')}
        onClick={withoutOpening(playback.togglePlay)}
      >
        {playing ? <PauseIcon /> : <PlayArrowIcon />}
      </IconButton>
      <IconButton
        className={classes.button}
        aria-label={translate('nowPlaying.next')}
        onClick={withoutOpening(playback.next)}
      >
        <SkipNextIcon />
      </IconButton>
      {!queued.isRadio && (
        <LinearProgress
          variant="determinate"
          value={percent}
          aria-label={translate('nowPlaying.progress')}
          classes={{ root: classes.progress, bar: classes.bar }}
        />
      )}
    </div>
  )
}

export default MiniPlayer
