import React from 'react'
import PropTypes from 'prop-types'
import { useTranslate } from 'react-admin'
import {
  Checkbox,
  FormControlLabel,
  List,
  ListItem,
  ListItemText,
  Typography,
} from '@material-ui/core'
import { Alert } from '@material-ui/lab'
import { makeStyles } from '@material-ui/core/styles'

const useStyles = makeStyles((theme) => ({
  root: { marginTop: theme.spacing(1) },
  list: { maxHeight: 240, overflowY: 'auto' },
  unpaired: { color: theme.palette.warning.main },
  options: { display: 'flex', flexDirection: 'column' },
}))

const number = (disc, track) => (disc > 0 ? `${disc}-${track}` : `${track}`)

// How the album's Tracks pair with the release picked with Find on MusicBrainz, shown before
// saving (match is the server's pairing, see dataProvider.matchMusicBrainzRelease): which release
// track each Track is, the Tracks that pair with none (they are left without track IDs), and the
// track IDs a File holds already that differ from the release's (kept, unless the admin says to
// replace them). The release's own title, album artist, date and track titles, where they differ,
// are offered, and written only if ticked; its cover, and the album's genres on MusicBrainz
// (if it has any), are offered ticked.
export const ReleasePairing = ({ match, options, onChange, disabled }) => {
  const classes = useStyles()
  const translate = useTranslate()
  const unpaired = match.tracks.filter((t) => !t.paired)
  const mismatches = match.tracks.filter((t) => t.mismatch)
  const { canonical } = match
  const set = (patch) => onChange({ ...options, ...patch })
  const setCanonical = (key) => (e) =>
    set({ canonical: { ...options.canonical, [key]: e.target.checked } })
  const option = (key, label, checked, onToggle) => (
    <FormControlLabel
      key={key}
      control={
        <Checkbox
          size="small"
          checked={checked}
          onChange={onToggle}
          disabled={disabled}
          inputProps={{ 'data-testid': `mbz-option-${key}` }}
        />
      }
      label={label}
    />
  )

  return (
    <div className={classes.root} data-testid="mbz-pairing">
      {!match.holds && (
        <Alert severity="warning" style={{ marginBottom: 8 }}>
          {translate('resources.album.musicbrainz.pairing.doesntHold')}
        </Alert>
      )}
      <Typography variant="subtitle2">
        {translate('resources.album.musicbrainz.pairing.title')}
      </Typography>
      <List dense className={classes.list} aria-label="Track pairing">
        {match.tracks.map((t) => (
          <ListItem key={t.id} data-testid={`mbz-pair-${t.id}`}>
            <ListItemText
              primary={`${number(t.disc, t.track)}. ${t.title}`}
              secondary={
                t.paired ? (
                  <>
                    {translate(
                      'resources.album.musicbrainz.pairing.pairedWith',
                      {
                        track: `${number(t.paired.disc, t.paired.position)}. ${t.paired.title}`,
                      },
                    )}
                    {t.mismatch && (
                      <span className={classes.unpaired}>
                        {' '}
                        {translate(
                          'resources.album.musicbrainz.pairing.mismatch',
                        )}
                      </span>
                    )}
                  </>
                ) : (
                  <span className={classes.unpaired}>
                    {translate('resources.album.musicbrainz.pairing.unpaired')}
                  </span>
                )
              }
            />
          </ListItem>
        ))}
      </List>
      {unpaired.length > 0 && (
        <Typography variant="body2" data-testid="mbz-unpaired">
          {translate('resources.album.musicbrainz.pairing.unpairedTracks', {
            smart_count: unpaired.length,
          })}
        </Typography>
      )}
      {match.unpaired.length > 0 && (
        <Typography variant="body2">
          {translate('resources.album.musicbrainz.pairing.unusedTracks', {
            smart_count: match.unpaired.length,
          })}
        </Typography>
      )}
      {!match.albumArtistMatches && (
        <Typography variant="body2">
          {translate('resources.album.musicbrainz.pairing.otherAlbumArtist', {
            artist: match.release.albumArtist,
          })}
        </Typography>
      )}
      <div className={classes.options}>
        {canonical.album.to &&
          option(
            'album',
            translate('resources.album.musicbrainz.pairing.useTitle', {
              value: canonical.album.to,
            }),
            options.canonical.album,
            setCanonical('album'),
          )}
        {canonical.albumArtist.to &&
          option(
            'albumArtist',
            translate('resources.album.musicbrainz.pairing.useAlbumArtist', {
              value: canonical.albumArtist.to,
            }),
            options.canonical.albumArtist,
            setCanonical('albumArtist'),
          )}
        {canonical.date.to &&
          option(
            'date',
            translate('resources.album.musicbrainz.pairing.useDate', {
              value: canonical.date.to,
            }),
            options.canonical.date,
            setCanonical('date'),
          )}
        {canonical.titles > 0 &&
          option(
            'titles',
            translate('resources.album.musicbrainz.pairing.useTitles', {
              smart_count: canonical.titles,
            }),
            options.canonical.titles,
            setCanonical('titles'),
          )}
        {mismatches.length > 0 &&
          option(
            'replaceTrackIds',
            translate('resources.album.musicbrainz.pairing.replaceTrackIds', {
              smart_count: mismatches.length,
            }),
            options.replaceTrackIds,
            (e) => set({ replaceTrackIds: e.target.checked }),
          )}
        {option(
          'cover',
          translate('resources.album.musicbrainz.pairing.writeCover'),
          options.cover,
          (e) => set({ cover: e.target.checked }),
        )}
        {match.genres?.length > 0 &&
          option(
            'genres',
            translate('resources.album.musicbrainz.pairing.writeGenres', {
              genres: match.genres.join(', '),
            }),
            options.genres,
            (e) => set({ genres: e.target.checked }),
          )}
      </div>
    </div>
  )
}

ReleasePairing.propTypes = {
  match: PropTypes.shape({
    holds: PropTypes.bool,
    tracks: PropTypes.array.isRequired,
    unpaired: PropTypes.array.isRequired,
    albumArtistMatches: PropTypes.bool,
    canonical: PropTypes.object.isRequired,
    release: PropTypes.object.isRequired,
    genres: PropTypes.arrayOf(PropTypes.string),
  }).isRequired,
  options: PropTypes.object.isRequired,
  onChange: PropTypes.func.isRequired,
  disabled: PropTypes.bool,
}

export default ReleasePairing
