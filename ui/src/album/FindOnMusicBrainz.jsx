import React, { useCallback, useEffect, useRef, useState } from 'react'
import PropTypes from 'prop-types'
import { useDataProvider, useTranslate } from 'react-admin'
import {
  Button,
  CircularProgress,
  List,
  ListItem,
  ListItemAvatar,
  ListItemText,
  Typography,
} from '@material-ui/core'
import { Alert } from '@material-ui/lab'
import { makeStyles } from '@material-ui/core/styles'
import AlbumIcon from '@material-ui/icons/Album'
import SearchIcon from '@material-ui/icons/Search'

const useStyles = makeStyles((theme) => ({
  root: { marginTop: theme.spacing(2) },
  list: { maxHeight: 280, overflowY: 'auto' },
  thumb: {
    width: 48,
    height: 48,
    objectFit: 'cover',
    display: 'block',
    borderRadius: 2,
  },
  noThumb: {
    width: 48,
    height: 48,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    color: theme.palette.text.disabled,
  },
  avatar: { minWidth: 60 },
  picked: { marginTop: theme.spacing(1) },
}))

// The server answers a failed search with a code; anything else is a generic error.
const knownErrors = ['busy', 'unavailable', 'disabled', 'invalid']

// A release's Cover Art Archive thumbnail. The archive is public, so the browser loads it
// directly, with no login token; a release with no cover shows a disc instead.
const Thumbnail = ({ url }) => {
  const classes = useStyles()
  const [failed, setFailed] = useState(false)
  if (!url || failed) {
    return (
      <span className={classes.noThumb} data-testid="mbz-no-thumbnail">
        <AlbumIcon />
      </span>
    )
  }
  return (
    <img
      className={classes.thumb}
      src={url}
      alt=""
      loading="lazy"
      referrerPolicy="no-referrer"
      onError={() => setFailed(true)}
    />
  )
}

Thumbnail.propTypes = { url: PropTypes.string }

const releaseDetail = (r) =>
  [r.year, r.country, r.format, r.status].filter(Boolean).join(' · ')

// Find on MusicBrainz, in the album Edit dialog: searches MusicBrainz's releases by the album
// artist and title as they are in the dialog, and lets the admin pick one. The pick is written
// into the album's Files with the rest of the dialog (#170). A failed search shows why, and
// leaves the rest of the dialog as it is.
export const FindOnMusicBrainz = ({
  artist,
  title,
  picked,
  onPick,
  disabled,
}) => {
  const classes = useStyles()
  const translate = useTranslate()
  const dataProvider = useDataProvider()
  const [releases, setReleases] = useState(null)
  const [searching, setSearching] = useState(false)
  const [error, setError] = useState(null)
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  const search = useCallback(() => {
    setSearching(true)
    setError(null)
    dataProvider
      .findMusicBrainzReleases(artist, title)
      .then(({ data }) => mounted.current && setReleases(data))
      .catch((err) => {
        if (!mounted.current) return
        setReleases(null)
        const code = err?.body?.error
        setError(
          translate(
            `resources.album.musicbrainz.errors.${knownErrors.includes(code) ? code : 'unknown'}`,
          ),
        )
      })
      .finally(() => mounted.current && setSearching(false))
  }, [dataProvider, artist, title, translate])

  return (
    <div className={classes.root}>
      <Button
        size="small"
        variant="outlined"
        onClick={search}
        disabled={disabled || searching || !title.trim()}
        startIcon={searching ? <CircularProgress size={14} /> : <SearchIcon />}
      >
        {translate('resources.album.musicbrainz.find')}
      </Button>
      {picked && (
        <Typography
          variant="body2"
          className={classes.picked}
          data-testid="mbz-picked"
        >
          {translate('resources.album.musicbrainz.picked', {
            release: picked.description,
          })}{' '}
          <Button size="small" onClick={() => onPick(null)} disabled={disabled}>
            {translate('resources.album.musicbrainz.clear')}
          </Button>
        </Typography>
      )}
      {error && (
        <Alert severity="warning" style={{ marginTop: 8 }}>
          {error}
        </Alert>
      )}
      {releases && releases.length === 0 && !error && (
        <Typography variant="body2" style={{ marginTop: 8 }}>
          {translate('resources.album.musicbrainz.noResults')}
        </Typography>
      )}
      {releases && releases.length > 0 && (
        <List dense className={classes.list} aria-label="MusicBrainz releases">
          {releases.map((r) => (
            <ListItem
              key={r.id}
              button
              selected={picked?.id === r.id}
              disabled={disabled}
              onClick={() => onPick(r)}
              data-testid={`mbz-release-${r.id}`}
            >
              <ListItemAvatar className={classes.avatar}>
                <Thumbnail url={r.thumbnailUrl} />
              </ListItemAvatar>
              <ListItemText
                primary={`${r.title} – ${r.artist}`}
                secondary={releaseDetail(r)}
              />
            </ListItem>
          ))}
        </List>
      )}
    </div>
  )
}

FindOnMusicBrainz.propTypes = {
  artist: PropTypes.string.isRequired,
  title: PropTypes.string.isRequired,
  picked: PropTypes.object,
  onPick: PropTypes.func.isRequired,
  disabled: PropTypes.bool,
}

export default FindOnMusicBrainz
