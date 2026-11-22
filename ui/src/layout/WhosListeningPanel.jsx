import React, { useState, useEffect, useCallback, useRef } from 'react'
import PropTypes from 'prop-types'
import { useSelector, useDispatch } from 'react-redux'
import { useTranslate, Link, useNotify } from 'react-admin'
import {
  Popover,
  IconButton,
  makeStyles,
  Tooltip,
  List,
  ListItem,
  Avatar,
  Badge,
  Card,
  CardContent,
  Typography,
  LinearProgress,
  useTheme,
  useMediaQuery,
} from '@material-ui/core'
import { FaRegCirclePlay, FaPause } from 'react-icons/fa6'
import subsonic from '../subsonic'
import { useInterval } from '../common'
import { nowPlayingCountSync } from '../actions'
import { formatDuration } from '../utils'
import config from '../config'

const useStyles = makeStyles((theme) => ({
  button: { color: 'inherit' },
  list: {
    width: '26em',
    maxHeight: (props) => {
      const entryHeight = 120
      const maxEntries = Math.min(props.entryCount || 0, 3)
      return maxEntries > 0 ? `${maxEntries * entryHeight}px` : '12em'
    },
    overflowY: 'auto',
    padding: 0,
  },
  card: {
    padding: 0,
  },
  cardContent: {
    padding: `${theme.spacing(1)}px !important`,
    '&:last-child': {
      paddingBottom: `${theme.spacing(1)}px !important`,
    },
  },
  listItem: {
    display: 'flex',
    alignItems: 'flex-start',
    gap: theme.spacing(1.5),
    padding: theme.spacing(1),
  },
  avatarContainer: {
    position: 'relative',
    flexShrink: 0,
    width: theme.spacing(8),
    height: theme.spacing(8),
  },
  avatar: {
    width: '100%',
    height: '100%',
    cursor: 'pointer',
    borderRadius: theme.spacing(0.5),
    '&:hover': {
      opacity: 0.8,
    },
  },
  stateOverlay: {
    position: 'absolute',
    top: 0,
    left: 0,
    width: '100%',
    height: '100%',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: 'rgba(0, 0, 0, 0.45)',
    borderRadius: theme.spacing(0.5),
    pointerEvents: 'none',
  },
  stateIcon: {
    color: 'rgba(255, 255, 255, 0.85)',
    fontSize: 18,
  },
  entryContent: {
    flex: 1,
    minWidth: 0,
    display: 'flex',
    flexDirection: 'column',
    gap: theme.spacing(0.25),
  },
  trackTitle: {
    fontWeight: 600,
    fontSize: '0.875rem',
    lineHeight: 1.3,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  trackDetail: {
    fontSize: '0.75rem',
    color: theme.palette.text.secondary,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  artistLink: {
    cursor: 'pointer',
    color: theme.palette.text.secondary,
    fontSize: '0.75rem',
    '&:hover': {
      textDecoration: 'underline',
    },
  },
  progressRow: {
    display: 'flex',
    alignItems: 'center',
    gap: theme.spacing(0.75),
    marginTop: theme.spacing(0.5),
  },
  progressTime: {
    fontSize: '0.65rem',
    color: theme.palette.text.secondary,
    fontVariantNumeric: 'tabular-nums',
    flexShrink: 0,
  },
  progressBar: {
    flex: 1,
    height: 3,
    borderRadius: 2,
    backgroundColor: theme.palette.action.disabledBackground,
    '& .MuiLinearProgress-bar': {
      borderRadius: 2,
    },
  },
  userInfo: {
    fontSize: '0.65rem',
    color: theme.palette.text.disabled,
    marginTop: theme.spacing(0.25),
  },
  badge: {
    '& .MuiBadge-badge': {
      backgroundColor: theme.palette.primary.main,
      color: theme.palette.primary.contrastText,
    },
  },
}))

// WhosListeningButton component - handles the button with badge
const WhosListeningButton = React.memo(({ count, onClick }) => {
  const classes = useStyles()
  const translate = useTranslate()

  return (
    <Tooltip title={translate('whosListening.title')}>
      <IconButton
        className={classes.button}
        onClick={onClick}
        aria-label={translate('whosListening.title')}
        aria-haspopup="true"
      >
        <Badge
          badgeContent={count}
          color="primary"
          overlap="rectangular"
          className={classes.badge}
        >
          <FaRegCirclePlay size={20} />
        </Badge>
      </IconButton>
    </Tooltip>
  )
})

WhosListeningButton.displayName = 'WhosListeningButton'

WhosListeningButton.propTypes = {
  count: PropTypes.number.isRequired,
  onClick: PropTypes.func.isRequired,
}

const WhosListeningItem = React.memo(
  ({ entry, onLinkClick, getArtistLink, now }) => {
    const classes = useStyles()
    const isPaused = entry.state === 'paused'
    const isPlaying = entry.state === 'playing' || entry.state === 'starting'
    const basePositionMs = entry.positionMs || 0
    const rate = entry.playbackRate || 1
    const elapsedSinceFetch = now - (entry._fetchedAt || now)
    const interpolatedMs = isPlaying
      ? basePositionMs + elapsedSinceFetch * rate
      : basePositionMs
    const durationMs = (entry.duration || 0) * 1000
    const clampedMs = Math.max(0, interpolatedMs)
    const positionMs =
      durationMs > 0 ? Math.min(clampedMs, durationMs) : clampedMs
    const positionSec = positionMs / 1000
    const durationSec = entry.duration || 0
    const progress = durationSec > 0 ? (positionSec / durationSec) * 100 : 0
    const artistId = entry.albumArtistId || entry.artistId
    const artistName = entry.albumArtist || entry.artist

    return (
      <ListItem className={classes.listItem}>
        <div className={classes.avatarContainer}>
          <Link to={`/album/${entry.albumId}/show`} onClick={onLinkClick}>
            <Avatar
              className={classes.avatar}
              src={subsonic.getCoverArtUrl(entry, 80)}
              variant="square"
              alt={`${entry.album} cover art`}
              loading="lazy"
            />
          </Link>
          {isPaused && (
            <div className={classes.stateOverlay}>
              <FaPause className={classes.stateIcon} />
            </div>
          )}
        </div>
        <div className={classes.entryContent}>
          <Typography className={classes.trackTitle} title={entry.title}>
            {entry.title}
          </Typography>
          {artistId ? (
            <Link
              to={getArtistLink(artistId)}
              className={classes.artistLink}
              onClick={onLinkClick}
            >
              {artistName}
            </Link>
          ) : (
            <Typography className={classes.trackDetail}>
              {artistName}
            </Typography>
          )}
          <Typography className={classes.trackDetail} title={entry.album}>
            {entry.album}
          </Typography>
          <div className={classes.progressRow}>
            <span className={classes.progressTime}>
              {formatDuration(positionSec)}
            </span>
            <LinearProgress
              className={classes.progressBar}
              variant="determinate"
              value={Math.min(progress, 100)}
            />
            <span className={classes.progressTime}>
              {formatDuration(durationSec)}
            </span>
          </div>
          <Typography className={classes.userInfo}>
            {entry.username}
            {entry.playerName ? ` (${entry.playerName})` : ''}
          </Typography>
        </div>
      </ListItem>
    )
  },
)

WhosListeningItem.displayName = 'WhosListeningItem'

WhosListeningItem.propTypes = {
  entry: PropTypes.shape({
    playerId: PropTypes.oneOfType([PropTypes.string, PropTypes.number])
      .isRequired,
    albumId: PropTypes.oneOfType([PropTypes.string, PropTypes.number])
      .isRequired,
    albumArtistId: PropTypes.oneOfType([PropTypes.string, PropTypes.number]),
    artistId: PropTypes.oneOfType([PropTypes.string, PropTypes.number]),
    albumArtist: PropTypes.string,
    artist: PropTypes.string,
    title: PropTypes.string.isRequired,
    username: PropTypes.string.isRequired,
    playerName: PropTypes.string,
    album: PropTypes.string,
    state: PropTypes.string,
    positionMs: PropTypes.number,
    duration: PropTypes.number,
  }).isRequired,
  onLinkClick: PropTypes.func.isRequired,
  getArtistLink: PropTypes.func.isRequired,
  now: PropTypes.number.isRequired,
}

// WhosListeningList component - handles the popover content
const WhosListeningList = React.memo(
  ({ anchorEl, open, onClose, entries, onLinkClick, getArtistLink, now }) => {
    const classes = useStyles({ entryCount: entries.length })
    const translate = useTranslate()

    return (
      <Popover
        id="panel-nowplaying"
        anchorEl={anchorEl}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
        transformOrigin={{ vertical: 'top', horizontal: 'right' }}
        open={open}
        onClose={onClose}
        PaperProps={{
          role: 'dialog',
          'aria-label': translate('whosListening.title'),
        }}
      >
        <Card className={classes.card}>
          <CardContent className={classes.cardContent}>
            {entries.length === 0 ? (
              <Typography id="now-playing-title">
                {translate('whosListening.empty')}
              </Typography>
            ) : (
              <List
                className={classes.list}
                dense
                aria-label={translate('whosListening.title')}
              >
                {entries.map((entry) => (
                  <WhosListeningItem
                    key={`${entry.username}-${entry.playerName}`}
                    entry={entry}
                    onLinkClick={onLinkClick}
                    getArtistLink={getArtistLink}
                    now={now}
                  />
                ))}
              </List>
            )}
          </CardContent>
        </Card>
      </Popover>
    )
  },
)

WhosListeningList.displayName = 'WhosListeningList'

WhosListeningList.propTypes = {
  anchorEl: PropTypes.object,
  open: PropTypes.bool.isRequired,
  onClose: PropTypes.func.isRequired,
  entries: PropTypes.arrayOf(PropTypes.object).isRequired,
  onLinkClick: PropTypes.func.isRequired,
  getArtistLink: PropTypes.func.isRequired,
  now: PropTypes.number.isRequired,
}

// Main WhosListeningPanel component
const WhosListeningPanel = () => {
  const dispatch = useDispatch()
  const count = useSelector((state) => state.activity.nowPlayingCount)
  const lastUpdate = useSelector((state) => state.activity.nowPlayingLastUpdate)
  const streamReconnected = useSelector(
    (state) => state.activity.streamReconnected,
  )
  const serverUp = useSelector(
    (state) => !!state.activity.serverStart.startTime,
  )
  const translate = useTranslate()
  const notify = useNotify()
  const theme = useTheme()
  const isSmallScreen = useMediaQuery(theme.breakpoints.down('sm'))

  const [anchorEl, setAnchorEl] = useState(null)
  const [entries, setEntries] = useState([])
  const [now, setNow] = useState(Date.now())
  const open = Boolean(anchorEl)

  const handleMenuOpen = useCallback((event) => {
    setAnchorEl(event.currentTarget)
  }, [])

  const handleMenuClose = useCallback(() => {
    setAnchorEl(null)
  }, [])

  // Close panel when link is clicked on small screens
  const handleLinkClick = useCallback(() => {
    if (isSmallScreen) {
      handleMenuClose()
    }
  }, [isSmallScreen, handleMenuClose])

  const getArtistLink = useCallback((artistId) => {
    if (!artistId) return null
    return config.devShowArtistPage && artistId !== config.variousArtistsId
      ? `/artist/${artistId}/show`
      : `/album?filter={"artist_id":"${artistId}"}&order=ASC&sort=max_year&displayedFilters={"compilation":true}&perPage=15`
  }, [])

  const fetchTimerRef = useRef(null)
  const doFetchRef = useRef()
  doFetchRef.current = () =>
    subsonic
      .getNowPlaying()
      .then((resp) => resp.json['subsonic-response'])
      .then((data) => {
        if (data.status === 'ok') {
          const fetchedEntries = data.nowPlaying?.entry || []
          const fetchTime = Date.now()
          setEntries(
            fetchedEntries.map((e) => ({ ...e, _fetchedAt: fetchTime })),
          )
          dispatch(nowPlayingCountSync({ count: fetchedEntries.length }))
        } else {
          throw new Error(
            data.error?.message || "Failed to fetch Who's Listening",
          )
        }
      })
      .catch((error) => {
        notify('ra.page.error', 'warning', {
          messageArgs: { error: error.message || 'Unknown error' },
        })
      })
  const fetchList = useCallback(() => {
    if (fetchTimerRef.current) clearTimeout(fetchTimerRef.current)
    fetchTimerRef.current = setTimeout(() => {
      fetchTimerRef.current = null
      doFetchRef.current()
    }, 300)
  }, [])

  useEffect(() => {
    return () => {
      if (fetchTimerRef.current) clearTimeout(fetchTimerRef.current)
    }
  }, [])

  // Initialize count and entries on mount, and refresh on server/stream changes
  useEffect(() => {
    if (serverUp) fetchList()
  }, [fetchList, serverUp, streamReconnected])

  // Refresh when Who's Listening updates from SSE events (if panel is open)
  useEffect(() => {
    if (open && serverUp) fetchList()
  }, [lastUpdate, open, fetchList, serverUp])

  // Update current time every second when open to animate progress bars
  useInterval(() => setNow(Date.now()), open ? 1000 : null)

  // Periodic refresh when panel is open (10 seconds)
  useInterval(
    () => {
      if (open && serverUp) fetchList()
    },
    open ? 10000 : null,
  )

  // Periodic refresh when panel is closed (60 seconds) to keep badge accurate
  useInterval(
    () => {
      if (!open && serverUp) fetchList()
    },
    !open ? 60000 : null,
  )

  return (
    <div>
      <WhosListeningButton count={count} onClick={handleMenuOpen} />
      <WhosListeningList
        anchorEl={anchorEl}
        open={open}
        onClose={handleMenuClose}
        entries={entries}
        now={now}
        onLinkClick={handleLinkClick}
        getArtistLink={getArtistLink}
      />
    </div>
  )
}

WhosListeningPanel.propTypes = {}

export default WhosListeningPanel
