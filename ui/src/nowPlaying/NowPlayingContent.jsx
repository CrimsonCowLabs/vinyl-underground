import React, { useEffect, useState } from 'react'
import { useSelector } from 'react-redux'
import { useHistory } from 'react-router-dom'
import { useTranslate } from 'react-admin'
import { Typography } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import subsonic from '../subsonic'
import { StarToggle, TrackRating } from './StarAndRating'
import { useTrackRecord } from './useTrackRecord'
import StationCard from './StationCard'
import { useStationDJ } from './useStationDJ'
import RecordArt from './RecordArt'
import Transport from './Transport'
import NowPlayingToolbar from './NowPlayingToolbar'
import { usePlayback } from './usePlayback'

const useStyles = makeStyles(
  (theme) => ({
    root: {
      minHeight: '100%',
      boxSizing: 'border-box',
      display: 'flex',
      flexDirection: 'column',
      gap: theme.spacing(1.5),
      padding: `calc(env(safe-area-inset-top) + ${theme.spacing(1)}px) ${theme.spacing(2.5)}px calc(env(safe-area-inset-bottom) + ${theme.spacing(1.5)}px)`,
      color: theme.palette.text.primary,
    },
    head: {
      display: 'grid',
      gridTemplateColumns: '48px minmax(0, 1fr) 48px',
      alignItems: 'center',
      textAlign: 'center',
    },
    source: {
      fontWeight: 700,
      whiteSpace: 'nowrap',
      overflow: 'hidden',
      textOverflow: 'ellipsis',
    },
    overline: { color: theme.palette.text.secondary, lineHeight: 1.6 },
    meta: { minWidth: 0 },
    stage: {
      flex: '1 1 auto',
      minHeight: 200,
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
    },
    track: {
      display: 'grid',
      gridTemplateColumns: 'minmax(0, 1fr) auto',
      columnGap: theme.spacing(1),
      alignItems: 'start',
    },
    side: { gridRow: 'span 3', marginRight: -12, marginTop: -6 },
    title: {
      fontSize: 22,
      fontWeight: 800,
      letterSpacing: '-0.015em',
      lineHeight: 1.2,
    },
    sub: {
      fontSize: 15,
      color: theme.palette.text.secondary,
      whiteSpace: 'nowrap',
      overflow: 'hidden',
      textOverflow: 'ellipsis',
    },
    link: {
      color: 'inherit',
      textDecoration: 'none',
      '&:hover, &:focus-visible': { textDecoration: 'underline' },
    },
    artist: { color: theme.palette.text.primary, fontWeight: 600 },
  }),
  { name: 'NDNowPlayingContent' },
)

const albumPath = (albumId) => albumId && `/album/${albumId}/show`

// titlePath is where the title goes: the album, which the header names, or an
// Internet Radio station's page
const titlePath = (current) =>
  current?.isRadio
    ? `/radio/${current.trackId}/show`
    : albumPath(current?.song?.albumId)

// NavLink is a real link (so it can be opened, read and focused as one) that
// goes through onNavigate, so the view can close as it navigates.
const NavLink = ({ path, onNavigate, className, children }) => {
  const router = useHistory()
  if (!path) return <span className={className}>{children}</span>
  return (
    <a
      href={router.createHref({ pathname: path })}
      className={className}
      onClick={(e) => {
        if (e.metaKey || e.ctrlKey || e.shiftKey) return
        e.preventDefault()
        onNavigate(path)
      }}
    >
      {children}
    </a>
  )
}

// useCover is the cover big enough for the view, falling back to the player
// bar's smaller one (which a Download keeps for its Tracks offline) if it can't be had.
const useCover = (current) => {
  const small = current?.cover
  const large =
    current?.isRadio || !current?.trackId
      ? small
      : subsonic.getCoverArtUrl(
          {
            id: current.trackId,
            updatedAt: current.song?.updatedAt,
            album: current.song?.album || ' ',
          },
          800,
        )
  const [failed, setFailed] = useState(null)
  useEffect(() => {
    if (!large || large === small) return
    const img = new Image()
    img.onerror = () => setFailed(large)
    img.src = large
    return () => {
      img.onerror = null
    }
  }, [large, small])
  return failed === large ? small : large
}

// trackShown is the Track playing or, before playback starts after a reload
// (the queue is kept, but nothing plays until the Listener presses play), the
// one queued to play
const trackShown = (state) => {
  const { current, queue, savedPlayIndex } = state.player
  return current?.uuid ? current : queue[savedPlayIndex || 0] || current
}

// NowPlayingContent is what Now Playing shows: the full-screen view on
// phones, and the side panel on desktop. closeButton is the host's own.
const NowPlayingContent = ({ audioInstance, onNavigate, closeButton }) => {
  const classes = useStyles()
  const translate = useTranslate()
  const current = useSelector(trackShown)
  const song = current?.song || {}
  const isRadio = !!current?.isRadio
  const record = useTrackRecord(current)
  const playback = usePlayback(audioInstance, current?.duration)
  const cover = useCover(current)
  const dj = useStationDJ()
  const title = isRadio ? current.name || song.name : song.title
  const overline = dj
    ? 'nowPlaying.station'
    : isRadio
      ? 'nowPlaying.radio'
      : 'nowPlaying.playingFromAlbum'
  const source = dj ? dj.name : isRadio ? title : song.album

  return (
    <div className={classes.root}>
      <div className={classes.head}>
        {closeButton}
        <div className={classes.meta}>
          <Typography
            variant="overline"
            component="div"
            className={classes.overline}
          >
            {translate(overline)}
          </Typography>
          <div className={classes.source}>{source}</div>
        </div>
      </div>
      <div className={classes.stage}>
        <RecordArt
          cover={cover}
          alt={translate('nowPlaying.cover', { name: song.album || title })}
          playing={playback.playing}
        />
      </div>
      <div className={classes.track}>
        <Typography component="h2" className={classes.title}>
          <NavLink
            path={titlePath(current)}
            onNavigate={onNavigate}
            className={classes.link}
          >
            {title}
          </NavLink>
        </Typography>
        {!isRadio && (
          <div className={classes.side}>
            <StarToggle record={record} />
          </div>
        )}
        {!isRadio && (
          <div className={classes.sub}>
            <NavLink
              path={song.artistId && `/artist/${song.artistId}/show`}
              onNavigate={onNavigate}
              className={`${classes.link} ${classes.artist}`}
            >
              {song.artist}
            </NavLink>
            {' · '}
            <NavLink
              path={albumPath(song.albumId)}
              onNavigate={onNavigate}
              className={classes.link}
            >
              {song.album}
            </NavLink>
          </div>
        )}
        {!isRadio && <TrackRating record={record} />}
      </div>
      <StationCard dj={dj} />
      <Transport playback={playback} seekable={!isRadio} />
      <NowPlayingToolbar onNavigate={onNavigate} />
    </div>
  )
}

export default NowPlayingContent
