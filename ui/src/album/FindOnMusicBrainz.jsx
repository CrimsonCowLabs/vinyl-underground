import React, { useCallback, useEffect, useRef, useState } from 'react'
import PropTypes from 'prop-types'
import { useDataProvider, useTranslate } from 'react-admin'
import {
  Button,
  Chip,
  CircularProgress,
  List,
  ListItem,
  ListItemAvatar,
  ListItemText,
  TextField,
  Typography,
} from '@material-ui/core'
import { Alert } from '@material-ui/lab'
import { makeStyles } from '@material-ui/core/styles'
import AlbumIcon from '@material-ui/icons/Album'
import SearchIcon from '@material-ui/icons/Search'

const useStyles = makeStyles((theme) => ({
  root: { marginTop: theme.spacing(2) },
  searchRow: {
    display: 'flex',
    alignItems: 'flex-start',
    gap: theme.spacing(1),
  },
  query: { flex: 1 },
  searchButton: { marginTop: theme.spacing(0.5), whiteSpace: 'nowrap' },
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
  fitting: {
    display: 'flex',
    alignItems: 'center',
    gap: theme.spacing(1),
    marginTop: theme.spacing(1),
  },
  best: { marginLeft: theme.spacing(1), height: 20 },
  fit: { display: 'block' },
}))

// The server answers a failed search with a code; anything else is a generic error.
const knownErrors = [
  'busy',
  'unavailable',
  'disabled',
  'invalid',
  'notFound',
  'notRelease',
]

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

// How many releases are fitted to the album's Files at most: the server looks each up, about a
// second apart.
const maxFitted = 5

// The releases worth fitting to the album's Files: those with as many tracks as the album has
// Tracks, or a disc with as many (the album may be one disc of it), the first in search order.
const fitCandidates = (releases, trackCount) =>
  releases
    .filter(
      (r) =>
        trackCount > 0 &&
        (r.trackCount === trackCount ||
          (r.discTrackCounts || []).includes(trackCount)),
    )
    .slice(0, maxFitted)
    .map((r) => r.id)

// Why no release is marked as the best match, once that is known: none has the album's number
// of Tracks, so none was checked (In Utero's hidden track, kept as its own File, makes 13 Tracks
// to MusicBrainz's 12); none checked pairs most of them; or the check failed. Null when one is
// marked, or there is nothing to say yet.
const noBestMatchReason = (releases, trackCount, candidates, fit) => {
  if (!releases?.length || !trackCount) return null
  if (!candidates.length) return 'noCandidates'
  // An answer for the list shown, or none yet
  if (fit.for !== releases) return null
  if (fit.error) return fit.error === 'busy' ? 'busy' : 'failed'
  return Object.values(fit.fits).some((f) => f.best) ? null : 'noGoodFit'
}

// Find on MusicBrainz, in the album Edit dialog: searches MusicBrainz's releases by what the
// admin types in its field (the album's title in the dialog until then; or a barcode, catalog
// number or release link, which the server tells apart), with the album artist as it is in the
// dialog (as Various Artists for a compilation), and lets the admin pick one. The pick is
// written into the album's Files with the rest of the dialog (#170). A failed search shows why,
// and leaves the rest of the dialog as it is. Of the releases with as many tracks as the album
// (trackCount, its Tracks once loaded), the server checks which fits the album's Files best, by
// the Tracks that pair with each and how close their lengths are; the list can be picked from
// meanwhile. When none is marked, a line says why.
export const FindOnMusicBrainz = ({
  albumId,
  trackCount,
  artist,
  title,
  compilation,
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
  // What the field searches: the dialog's title, until the admin types in it
  const [query, setQuery] = useState(title)
  const typed = useRef(false)
  useEffect(() => {
    if (!typed.current) setQuery(title)
  }, [title])
  // How each release fitted fits the album's Files, by ID, or why that couldn't be had (an
  // error code), for the list of releases they were checked for; and whether the server is at it
  const [fit, setFit] = useState({ for: null, fits: {}, error: null })
  const fits = fit.for === releases ? fit.fits : {}
  const [fitting, setFitting] = useState(false)
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  // An answer for a search the admin has since run again is dropped.
  useEffect(() => {
    setFit({ for: null, fits: {}, error: null })
    setFitting(false)
    if (!releases || !albumId) return
    const ids = fitCandidates(releases, trackCount)
    if (!ids.length) return
    let current = true
    setFitting(true)
    dataProvider
      .fitMusicBrainzReleases(albumId, ids)
      .then(({ data }) => {
        if (!current) return
        // None scored: every lookup failed, though MusicBrainz wasn't busy for all
        setFit({
          for: releases,
          fits: Object.fromEntries((data || []).map((f) => [f.id, f])),
          error: data?.length ? null : 'unavailable',
        })
      })
      // The marks only help to pick: the list is the same without them, and a line says why
      .catch((err) => {
        if (!current) return
        setFit({
          for: releases,
          fits: {},
          error: err?.body?.error || 'unknown',
        })
      })
      .finally(() => current && setFitting(false))
    return () => {
      current = false
    }
  }, [dataProvider, releases, albumId, trackCount])

  const fitNote = (f) => {
    const paired = translate('resources.album.musicbrainz.fit.paired', {
      smart_count: f.albumTracks,
      paired: f.paired,
    })
    if (f.lengthDiff === null || f.lengthDiff === undefined) return paired
    return `${paired} · ${translate('resources.album.musicbrainz.fit.lengths', {
      seconds: f.lengthDiff.toFixed(1),
    })}`
  }

  const search = useCallback(() => {
    setSearching(true)
    setError(null)
    dataProvider
      .findMusicBrainzReleases(artist, query.trim(), !!compilation)
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
  }, [dataProvider, artist, query, compilation, translate])
  const canSearch = !disabled && !searching && !!query.trim()

  const noBestMatch =
    !fitting &&
    noBestMatchReason(
      releases,
      trackCount,
      releases ? fitCandidates(releases, trackCount) : [],
      fit,
    )

  return (
    <div className={classes.root}>
      <div className={classes.searchRow}>
        <TextField
          className={classes.query}
          size="small"
          variant="outlined"
          label={translate('resources.album.musicbrainz.search')}
          helperText={translate('resources.album.musicbrainz.searchHint')}
          value={query}
          onChange={(e) => {
            typed.current = true
            setQuery(e.target.value)
          }}
          onKeyDown={(e) => {
            // Enter searches, rather than saving the dialog
            if (e.key !== 'Enter') return
            e.preventDefault()
            if (canSearch) search()
          }}
          disabled={disabled}
          inputProps={{ 'data-testid': 'mbz-query' }}
        />
        <Button
          size="small"
          variant="outlined"
          className={classes.searchButton}
          onClick={search}
          disabled={!canSearch}
          startIcon={
            searching ? <CircularProgress size={14} /> : <SearchIcon />
          }
        >
          {translate('resources.album.musicbrainz.find')}
        </Button>
      </div>
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
      {fitting && (
        <Typography
          variant="body2"
          component="div"
          color="textSecondary"
          className={classes.fitting}
          data-testid="mbz-fitting"
        >
          <CircularProgress size={12} />
          {translate('resources.album.musicbrainz.fitting')}
        </Typography>
      )}
      {releases && releases.length > 0 && (
        <List dense className={classes.list} aria-label="MusicBrainz releases">
          {releases.map((r) => {
            const fit = fits[r.id]
            return (
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
                  primary={
                    <>
                      {`${r.title} – ${r.artist}`}
                      {fit?.best && (
                        <Chip
                          component="span"
                          size="small"
                          color="primary"
                          className={classes.best}
                          label={translate(
                            'resources.album.musicbrainz.bestMatch',
                          )}
                          data-testid="mbz-best-match"
                        />
                      )}
                    </>
                  }
                  secondary={
                    <>
                      {releaseDetail(r)}
                      {fit && (
                        <span
                          className={classes.fit}
                          data-testid={`mbz-fit-${r.id}`}
                        >
                          {fitNote(fit)}
                        </span>
                      )}
                    </>
                  }
                />
              </ListItem>
            )
          })}
        </List>
      )}
      {noBestMatch && (
        <Typography
          variant="body2"
          color="textSecondary"
          className={classes.picked}
          data-testid="mbz-no-best-match"
        >
          {translate(
            `resources.album.musicbrainz.noBestMatch.${noBestMatch}`,
            noBestMatch === 'noCandidates' || noBestMatch === 'noGoodFit'
              ? { smart_count: trackCount }
              : undefined,
          )}
        </Typography>
      )}
    </div>
  )
}

FindOnMusicBrainz.propTypes = {
  albumId: PropTypes.string,
  trackCount: PropTypes.number,
  artist: PropTypes.string.isRequired,
  title: PropTypes.string.isRequired,
  compilation: PropTypes.bool,
  picked: PropTypes.object,
  onPick: PropTypes.func.isRequired,
  disabled: PropTypes.bool,
}

export default FindOnMusicBrainz
