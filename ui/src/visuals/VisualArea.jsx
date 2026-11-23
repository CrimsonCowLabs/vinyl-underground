import React, { useEffect, useRef, useState } from 'react'
import { useTranslate } from 'react-admin'
import { Dialog, IconButton, Typography } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import FullscreenIcon from '@material-ui/icons/Fullscreen'
import FullscreenExitIcon from '@material-ui/icons/FullscreenExit'
import RecordArt from '../nowPlaying/RecordArt'
import { nowPlayingBackground } from '../nowPlaying/vuPalette'
import VisualCanvas from './VisualCanvas'
import { GLOW_SPREAD } from './renderers'
import { visualMode } from './visualModes'
import { useVisualAnalyser } from './useVisualAnalyser'
import { useVisualTheme } from './useVisualTheme'

// In Full screen, the glow's cover takes this much of the window's shorter
// side, and the glow the whole window
const FULL_COVER_SHARE = 0.56

// How far the glow's canvas reaches past each edge of the cover
const GLOW_INSET = `${(-(GLOW_SPREAD - 1) / 2) * 100}%`

const useStyles = makeStyles(
  (theme) => ({
    waiting: {
      display: 'flex',
      flexDirection: 'column',
      alignItems: 'center',
      gap: theme.spacing(1.5),
    },
    note: { color: theme.palette.text.secondary, textAlign: 'center' },
    // The same size as RecordArt's sleeve, so turning a visual on or off
    // never moves what's below
    area: {
      position: 'relative',
      width: 'min(68cqi, 38vh, 420px)',
      aspectRatio: '1 / 1',
    },
    cover: {
      position: 'relative',
      display: 'block',
      width: '100%',
      height: '100%',
      objectFit: 'cover',
      borderRadius: 6,
      backgroundColor: theme.palette.background.paper,
      boxShadow: '0 18px 40px rgba(0, 0, 0, 0.45)',
    },
    // Under spectrum and waveform: the cover, dimmed so the visual reads
    dimmed: { filter: 'brightness(0.32) saturate(1.2)' },
    // In the glow: lit by it rather than shadowed
    glowing: { boxShadow: 'none' },
    over: {
      position: 'absolute',
      inset: 0,
      width: '100%',
      height: '100%',
      borderRadius: 6,
    },
    around: {
      position: 'absolute',
      inset: GLOW_INSET,
      width: `${GLOW_SPREAD * 100}%`,
      height: `${GLOW_SPREAD * 100}%`,
      pointerEvents: 'none',
    },
    expand: {
      position: 'absolute',
      top: 6,
      right: 6,
      padding: 8,
      color: '#fff',
      backgroundColor: 'rgba(0, 0, 0, 0.45)',
      '&:hover': { backgroundColor: 'rgba(0, 0, 0, 0.6)' },
    },
    // Full screen: the whole window, or the whole screen where the browser
    // allows it
    full: {
      position: 'relative',
      overflow: 'hidden',
      backgroundColor: theme.palette.background.default,
      backgroundImage: nowPlayingBackground(theme),
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
    },
    backdrop: {
      position: 'absolute',
      inset: 0,
      width: '100%',
      height: '100%',
      objectFit: 'cover',
      filter: 'blur(28px) brightness(0.3) saturate(1.2)',
      transform: 'scale(1.1)',
    },
    fullCanvas: {
      position: 'absolute',
      inset: 0,
      width: '100%',
      height: '100%',
    },
    fullArea: {
      position: 'relative',
      width: `${FULL_COVER_SHARE * 100}vmin`,
      aspectRatio: '1 / 1',
    },
    exit: {
      position: 'absolute',
      top: 'calc(env(safe-area-inset-top) + 12px)',
      right: 'calc(env(safe-area-inset-right) + 12px)',
      color: '#fff',
      backgroundColor: 'rgba(0, 0, 0, 0.45)',
      '&:hover': { backgroundColor: 'rgba(0, 0, 0, 0.6)' },
    },
  }),
  { name: 'NDVisualArea' },
)

const fullscreenElement = () =>
  document.fullscreenElement || document.webkitFullscreenElement || null
const FULLSCREEN_EVENTS = ['fullscreenchange', 'webkitfullscreenchange']

// useBrowserFullScreen puts `element` into the browser's full screen where
// the browser supports it (not an iPhone), and calls onLeave when the
// Listener leaves it (Esc, or the browser's own control). Elsewhere the
// element simply fills the window.
const useBrowserFullScreen = (element, onLeave) => {
  const onLeaveRef = useRef(onLeave)
  onLeaveRef.current = onLeave
  useEffect(() => {
    const request =
      element?.requestFullscreen || element?.webkitRequestFullscreen
    if (!request) return
    const onChange = () => {
      if (!fullscreenElement()) onLeaveRef.current()
    }
    FULLSCREEN_EVENTS.forEach((e) => document.addEventListener(e, onChange))
    // Chrome can leave focus on the page's body in full screen, out of the
    // Dialog's reach, so Esc is caught here too when the browser passes it on
    const onKeyDown = (e) => {
      if (e.key === 'Escape') onLeaveRef.current()
    }
    document.addEventListener('keydown', onKeyDown)
    try {
      // Refused (no user gesture, or not allowed): the window is filled all
      // the same
      request.call(element)?.catch?.(() => {})
    } catch {
      // As above, from a browser that throws rather than rejects
    }
    return () => {
      FULLSCREEN_EVENTS.forEach((e) =>
        document.removeEventListener(e, onChange),
      )
      document.removeEventListener('keydown', onKeyDown)
      if (fullscreenElement() === element) {
        const exit = document.exitFullscreen || document.webkitExitFullscreen
        Promise.resolve()
          .then(() => exit?.call(document))
          .catch(() => {})
      }
    }
  }, [element])
}

// FullScreenVisual is the visual over the whole window, above Now Playing.
// Esc, or its button, returns to Now Playing.
const FullScreenVisual = ({
  mode,
  analyser,
  theme,
  cover,
  alt,
  playing,
  onClose,
}) => {
  const classes = useStyles()
  const translate = useTranslate()
  const [paper, setPaper] = useState(null)
  useBrowserFullScreen(paper, onClose)
  const { surround } = visualMode(mode)

  return (
    <Dialog
      fullScreen
      open
      onClose={onClose}
      PaperProps={{
        ref: setPaper,
        className: classes.full,
        'aria-label': translate('nowPlaying.visuals.fullScreen'),
      }}
    >
      {surround === 'over' ? (
        <>
          {cover && (
            <img
              className={classes.backdrop}
              src={cover}
              alt=""
              aria-hidden="true"
            />
          )}
          <VisualCanvas
            mode={mode}
            analyser={analyser}
            theme={theme}
            playing={playing}
            className={classes.fullCanvas}
          />
        </>
      ) : (
        <>
          <VisualCanvas
            mode={mode}
            analyser={analyser}
            theme={theme}
            playing={playing}
            className={classes.fullCanvas}
            coverShare={FULL_COVER_SHARE}
          />
          <div className={classes.fullArea}>
            <Cover classes={classes} cover={cover} alt={alt} glowing />
          </div>
        </>
      )}
      <IconButton
        className={classes.exit}
        aria-label={translate('nowPlaying.visuals.exitFullScreen')}
        onClick={onClose}
      >
        <FullscreenExitIcon />
      </IconButton>
    </Dialog>
  )
}

const Cover = ({ classes, cover, alt, dimmed = false, glowing = false }) => {
  const className = [
    classes.cover,
    dimmed && classes.dimmed,
    glowing && classes.glowing,
  ]
    .filter(Boolean)
    .join(' ')
  const label = dimmed ? { alt: '', 'aria-hidden': true } : { alt }
  return cover ? (
    <img className={className} src={cover} {...label} />
  ) : (
    <div
      className={className}
      role={dimmed ? undefined : 'img'}
      aria-label={dimmed ? undefined : alt}
    />
  )
}

// VisualArea is Now Playing's art area with a visual playing: spectrum and
// waveform over the dimmed cover, the glow around it. A Full screen button
// expands it. Until the shared audio chain can be read (Internet Radio waits
// for the next Library Track; a browser without Web Audio never can) it
// shows the album art as RecordArt does.
const VisualArea = ({ mode, cover, alt, playing, audioInstance }) => {
  const classes = useStyles()
  const translate = useTranslate()
  const { analyser, waiting } = useVisualAnalyser(audioInstance)
  const theme = useVisualTheme(cover)
  const [expanded, setExpanded] = useState(false)

  if (!analyser) {
    if (!waiting) return <RecordArt cover={cover} alt={alt} playing={playing} />
    return (
      <div className={classes.waiting}>
        <RecordArt cover={cover} alt={alt} playing={playing} />
        <Typography variant="body2" className={classes.note}>
          {translate('nowPlaying.visuals.radioWaits')}
        </Typography>
      </div>
    )
  }

  const { surround } = visualMode(mode)
  // While expanded, the art area keeps its place but doesn't draw
  const canvas = !expanded && (
    <VisualCanvas
      mode={mode}
      analyser={analyser}
      theme={theme}
      playing={playing}
      className={surround === 'over' ? classes.over : classes.around}
    />
  )

  return (
    <div className={classes.area}>
      {surround === 'over' ? (
        <>
          <Cover classes={classes} cover={cover} alt={alt} dimmed />
          {canvas}
        </>
      ) : (
        <>
          {canvas}
          <Cover classes={classes} cover={cover} alt={alt} glowing />
        </>
      )}
      <IconButton
        className={classes.expand}
        aria-label={translate('nowPlaying.visuals.fullScreen')}
        onClick={() => setExpanded(true)}
      >
        <FullscreenIcon fontSize="small" />
      </IconButton>
      {expanded && (
        <FullScreenVisual
          mode={mode}
          analyser={analyser}
          theme={theme}
          cover={cover}
          alt={alt}
          playing={playing}
          onClose={() => setExpanded(false)}
        />
      )}
    </div>
  )
}

export default VisualArea
