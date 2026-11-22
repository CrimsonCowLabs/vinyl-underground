import React, { useEffect, useRef } from 'react'
import { useTranslate } from 'react-admin'
import { makeStyles } from '@material-ui/core/styles'
import clsx from 'clsx'
import { currentLine } from './lyrics'
import { useLyrics } from './useLyrics'

const fade = 'linear-gradient(transparent, #000 18%, #000 75%, transparent)'

const useStyles = makeStyles(
  (theme) => ({
    scroller: {
      position: 'absolute',
      inset: 0,
      overflowY: 'auto',
      overscrollBehavior: 'contain',
      scrollBehavior: 'smooth',
      '@media (prefers-reduced-motion: reduce)': { scrollBehavior: 'auto' },
    },
    synced: {
      // Room above and below, so the first and last lines can sit at the
      // reading line, and the edges fade
      padding: '30% 4px 40%',
      maskImage: fade,
      WebkitMaskImage: fade,
    },
    unsynced: { padding: '8px 4px 24px' },
    line: {
      display: 'block',
      width: '100%',
      margin: '0 0 14px',
      padding: 0,
      border: 0,
      background: 'none',
      textAlign: 'left',
      font: 'inherit',
      fontSize: 22,
      lineHeight: 1.25,
      fontWeight: 800,
      letterSpacing: '-0.01em',
      color: theme.palette.text.disabled,
      cursor: 'pointer',
      transition: 'color 0.3s',
      '&:focus-visible': {
        outline: `2px solid ${theme.palette.primary.main}`,
        outlineOffset: 2,
      },
    },
    past: { color: theme.palette.text.secondary },
    now: { color: theme.palette.text.primary },
    text: {
      margin: 0,
      minHeight: '1.4em',
      fontSize: 17,
      lineHeight: 1.4,
      fontWeight: 600,
      color: theme.palette.text.primary,
      whiteSpace: 'pre-wrap',
    },
    note: {
      fontSize: 12,
      color: theme.palette.text.secondary,
      marginBottom: 14,
    },
    empty: {
      alignSelf: 'center',
      textAlign: 'center',
      fontSize: 15,
      color: theme.palette.text.secondary,
    },
  }),
  { name: 'NDNowPlayingLyrics' },
)

// SyncedLyrics highlights the line being sung, keeping it in view a little
// above the middle, and seeks to a line when it is tapped.
const SyncedLyrics = ({ lines, playback }) => {
  const classes = useStyles()
  const scroller = useRef(null)
  const now = currentLine(lines, playback.currentTime)

  useEffect(() => {
    const box = scroller.current
    const line = box?.children[Math.max(now, 0)]
    if (!line) return
    box.scrollTop = line.offsetTop - box.clientHeight * 0.38
  }, [now])

  return (
    <div
      ref={scroller}
      className={clsx(classes.scroller, classes.synced)}
      data-no-swipe
    >
      {lines.map((line, i) => (
        <button
          key={i}
          type="button"
          className={clsx(
            classes.line,
            i < now && classes.past,
            i === now && classes.now,
          )}
          aria-current={i === now ? 'true' : undefined}
          onClick={() => playback.seek(line.time)}
        >
          {line.text || '♪'}
        </button>
      ))}
    </div>
  )
}

// UnsyncedLyrics are plain text to scroll through, as they have no times.
const UnsyncedLyrics = ({ lines }) => {
  const classes = useStyles()
  const translate = useTranslate()
  return (
    <div className={clsx(classes.scroller, classes.unsynced)} data-no-swipe>
      <div className={classes.note}>
        {translate('nowPlaying.lyrics.notSynced')}
      </div>
      {lines.map((line, i) => (
        <p key={i} className={classes.text}>
          {line}
        </p>
      ))}
    </div>
  )
}

// LyricsView is Now Playing's Lyrics tab for the Track shown.
const LyricsView = ({ current, playback }) => {
  const classes = useStyles()
  const translate = useTranslate()
  const { lyrics, loading } = useLyrics(current)
  if (loading) return null
  if (!lyrics) {
    return (
      <div className={classes.empty}>{translate('nowPlaying.lyrics.none')}</div>
    )
  }
  return lyrics.synced ? (
    <SyncedLyrics lines={lyrics.lines} playback={playback} />
  ) : (
    <UnsyncedLyrics lines={lyrics.lines} />
  )
}

export default LyricsView
