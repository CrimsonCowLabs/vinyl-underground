import React, { useCallback, useEffect, useState } from 'react'
import PropTypes from 'prop-types'
import {
  Button,
  useDataProvider,
  useNotify,
  usePermissions,
  useRedirect,
  useRefresh,
  useTranslate,
} from 'react-admin'
import {
  Button as MuiButton,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  TextField,
} from '@material-ui/core'
import { Alert, Autocomplete } from '@material-ui/lab'
import EditIcon from '@material-ui/icons/Edit'
import { useArtistSuggestions } from '../artist/useArtistSuggestions'
import { TrackNumbersEditor } from './TrackNumbersEditor'
import {
  markSaved,
  trackNumberChanges,
  trackNumbersValid,
  useTrackNumberRows,
} from './trackNumbers'
import { FindOnMusicBrainz } from './FindOnMusicBrainz'
import { ReleasePairing } from './ReleasePairing'
import { useLibraryEditing } from '../library/useLibraryEditing'

// The server answers a refused save with a code; anything else is a generic error.
const knownErrors = [
  'sharedFolder',
  'renamedAway',
  'alreadyCorrected',
  'invalid',
  'notFound',
  'editingOff',
  'unsupportedFormat',
  'changedOnDisk',
  // MusicBrainz couldn't be asked for the picked release
  'busy',
  'unavailable',
  'disabled',
]

// The pairing's own failures are MusicBrainz's codes, or the album's.
const knownMatchErrors = ['busy', 'unavailable', 'disabled', 'notFound']

// The options a save with a picked release starts with: the release's IDs, its cover and the
// album's MusicBrainz genres, keeping what the Files hold otherwise (#170).
const noReleaseOptions = {
  canonical: { album: false, albumArtist: false, date: false, titles: false },
  replaceTrackIds: false,
  cover: true,
  genres: true,
}

// What the Cover Art Archive answered for a picked release's cover, when it was asked for and gave
// none.
const pickCoverNotes = {
  none: 'resources.album.musicbrainz.noCover',
  failed: 'resources.album.musicbrainz.coverFailed',
  disabled: 'resources.album.musicbrainz.coverDisabled',
}

// The year clients show for the album: its original year when it has one, as the server
// compares it.
const albumYear = (record) => record.maxOriginalYear || record.maxYear || ''

// The album Edit dialog: writes the album's title, album artist (offered from the existing
// Artists as you type) and year into its Files, and its Tracks' changed disc and track numbers
// into theirs, as one edit job (#163). A release picked with Find on MusicBrainz is written into
// the Files in the same job (#170): its IDs, its cover and genres unless unticked, and, if ticked, its own values. The dialog
// shows how the album's Tracks pair with it before saving.
const EditAlbumDialog = ({ record, onClose }) => {
  const translate = useTranslate()
  const dataProvider = useDataProvider()
  const notify = useNotify()
  const redirect = useRedirect()
  const refresh = useRefresh()
  const [trackRows, setTrackRows] = useTrackNumberRows(record.id)
  const [name, setName] = useState(record.name || '')
  const [albumArtist, setAlbumArtist] = useState(record.albumArtist || '')
  const [year, setYear] = useState(String(albumYear(record)))
  const [release, setRelease] = useState(null)
  // The pairing of the picked release (null while it loads), why it couldn't be had (an error
  // code), and what the admin ticks
  const [match, setMatch] = useState(null)
  const [matchError, setMatchError] = useState(null)
  const [releaseOptions, setReleaseOptions] = useState(noReleaseOptions)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState(null)
  const suggestions = useArtistSuggestions(albumArtist)

  useEffect(() => {
    setMatch(null)
    setMatchError(null)
    setReleaseOptions(noReleaseOptions)
    if (!release) return
    let current = true
    dataProvider
      .matchMusicBrainzRelease(release.id, record.id)
      .then(({ data }) => current && setMatch(data))
      .catch((err) => {
        if (!current) return
        const code = err?.body?.error
        setMatchError(knownMatchErrors.includes(code) ? code : 'unknown')
      })
    return () => {
      current = false
    }
  }, [dataProvider, release, record.id])
  const accepted = releaseOptions.canonical

  const yearValue = year.trim() === '' ? 0 : Number(year)
  const yearValid =
    year.trim() === '' ||
    (/^\d{4}$/.test(year.trim()) && yearValue >= 1000 && yearValue <= 9999)
  // Spaces are folded as the server folds them
  const artist = albumArtist.split(/\s+/).filter(Boolean).join(' ')
  const unchanged =
    name.trim() === (record.name || '') &&
    artist === (record.albumArtist || '') &&
    (yearValue === 0 || yearValue === Number(albumYear(record) || 0)) &&
    !(release && match)
  const trackChanges = trackNumberChanges(trackRows || [])
  const canSave =
    !saving &&
    // A picked release is saved once the admin has seen how it pairs; one whose pairing
    // couldn't be had is left out, and the rest saved
    (!release || match || matchError) &&
    name.trim() &&
    artist &&
    yearValid &&
    trackNumbersValid(trackRows || []) &&
    (!unchanged || trackChanges.length > 0)

  const handleSave = useCallback(
    (e) => {
      e.preventDefault()
      setSaving(true)
      setError(null)
      // One save is one edit job: the album's values and the Tracks' changed numbers together
      const save = unchanged
        ? dataProvider.correctTrackNumbers(record.id, trackChanges)
        : dataProvider.correctAlbum(record.id, {
            name: name.trim(),
            albumArtist: artist,
            year: yearValue,
            ...(trackChanges.length && { tracks: trackChanges }),
            ...(release &&
              match && {
                mbzAlbumId: release.id,
                ...releaseOptions,
              }),
          })
      save
        .then(({ data }) => {
          setTrackRows(markSaved)
          onClose()
          const skipped = data.job?.skipped || 0
          // Files the job couldn't write (the Activity panel lists them, and why), or a picked
          // release whose cover couldn't be had, are worth a word over the usual one. A release
          // that couldn't reach every File was taken out of those it reached, so the album
          // isn't split: nothing changed
          const note = data.rolledBack
            ? 'resources.album.musicbrainz.rolledBack'
            : skipped
              ? 'resources.album.editor.skipped'
              : pickCoverNotes[data.pickCover]
          const level = note ? 'warning' : 'info'
          const opts = skipped
            ? { type: level, messageArgs: { smart_count: skipped } }
            : { type: level }
          if (data.done && (!data.albumId || data.albumId === record.id)) {
            // The album keeps its ID (e.g. only track numbers changed): its page reloads
            notify(note || 'resources.album.editor.corrected', opts)
            refresh()
          } else if (data.done) {
            notify(note || 'resources.album.editor.corrected', opts)
            redirect(`/album/${data.albumId}/show`)
          } else {
            notify(note || 'resources.album.editor.correcting', opts)
            // The job finishes in the background (see the Activity panel), and the album's new
            // values take this page's album ID with them
            if (!unchanged) redirect('/album')
          }
        })
        .catch((err) => {
          const code = err?.body?.error
          setError(
            translate(
              `resources.album.editor.errors.${knownErrors.includes(code) ? code : 'unknown'}`,
              { name: artist },
            ),
          )
          setSaving(false)
        })
    },
    [
      dataProvider,
      record.id,
      name,
      artist,
      yearValue,
      unchanged,
      trackChanges,
      setTrackRows,
      release,
      match,
      releaseOptions,
      onClose,
      notify,
      redirect,
      refresh,
      translate,
    ],
  )

  return (
    <Dialog
      open
      onClose={onClose}
      fullWidth
      maxWidth={trackRows?.length ? 'sm' : 'xs'}
      aria-labelledby="edit-album-title"
    >
      <form onSubmit={handleSave}>
        <DialogTitle id="edit-album-title">
          {translate('resources.album.actions.edit')}
        </DialogTitle>
        <DialogContent>
          <DialogContentText>
            {translate('resources.album.editor.help')}
          </DialogContentText>
          <TextField
            autoFocus
            fullWidth
            margin="dense"
            label={translate('resources.album.fields.name')}
            value={name}
            onChange={(e) => setName(e.target.value)}
            disabled={saving || (!!release && accepted.album)}
            inputProps={{ 'data-testid': 'album-name' }}
          />
          <Autocomplete
            freeSolo
            disableClearable
            options={suggestions}
            filterOptions={(options) => options}
            getOptionLabel={(a) => (typeof a === 'string' ? a : a.name)}
            value={albumArtist}
            onChange={(_, value) =>
              setAlbumArtist(
                typeof value === 'string' ? value : value?.name || '',
              )
            }
            inputValue={albumArtist}
            onInputChange={(_, value) => setAlbumArtist(value)}
            disabled={saving || (!!release && accepted.albumArtist)}
            renderInput={(params) => (
              <TextField
                {...params}
                fullWidth
                margin="dense"
                label={translate('resources.album.fields.albumArtist')}
                inputProps={{
                  ...params.inputProps,
                  'data-testid': 'album-artist',
                }}
              />
            )}
          />
          <TextField
            fullWidth
            margin="dense"
            label={translate('resources.album.fields.year')}
            value={year}
            onChange={(e) => setYear(e.target.value)}
            disabled={saving || (!!release && accepted.date)}
            error={!yearValid}
            inputProps={{ inputMode: 'numeric', 'data-testid': 'album-year' }}
          />
          <FindOnMusicBrainz
            artist={artist}
            title={name}
            picked={release}
            onPick={setRelease}
            disabled={saving}
          />
          {release && !match && !matchError && (
            <CircularProgress size={16} data-testid="mbz-pairing-loading" />
          )}
          {matchError && (
            <Alert severity="warning" style={{ marginTop: 8 }}>
              {translate(`resources.album.musicbrainz.errors.${matchError}`)}
            </Alert>
          )}
          {release && match && (
            <ReleasePairing
              match={match}
              options={releaseOptions}
              onChange={setReleaseOptions}
              disabled={saving}
            />
          )}
          <TrackNumbersEditor
            rows={trackRows}
            onChange={setTrackRows}
            disabled={saving}
          />
          {error && (
            <Alert severity="error" style={{ marginTop: 16 }}>
              {error}
            </Alert>
          )}
        </DialogContent>
        <DialogActions>
          <MuiButton onClick={onClose} disabled={saving}>
            {translate('ra.action.cancel')}
          </MuiButton>
          <MuiButton
            type="submit"
            color="primary"
            variant="contained"
            disabled={!canSave}
            startIcon={saving ? <CircularProgress size={16} /> : null}
          >
            {translate('ra.action.save')}
          </MuiButton>
        </DialogActions>
      </form>
    </Dialog>
  )
}

EditAlbumDialog.propTypes = {
  record: PropTypes.object.isRequired,
  onClose: PropTypes.func.isRequired,
}

// "Edit" on an album page (admin only, while the album's library is writable: ADR-0011): writes
// the album's values into its Files.
export const EditAlbumButton = ({ record, className, size }) => {
  const translate = useTranslate()
  const { permissions } = usePermissions()
  const { isWritable } = useLibraryEditing()
  const [open, setOpen] = useState(false)
  const handleClose = useCallback(() => setOpen(false), [])

  if (
    permissions !== 'admin' ||
    !record?.id ||
    isWritable(record.libraryId) !== true
  )
    return null

  return (
    <>
      <Button
        className={className}
        size={size}
        onClick={() => setOpen(true)}
        label={translate('resources.album.actions.edit')}
      >
        <EditIcon />
      </Button>
      {open && <EditAlbumDialog record={record} onClose={handleClose} />}
    </>
  )
}

EditAlbumButton.propTypes = {
  record: PropTypes.object,
  className: PropTypes.string,
  size: PropTypes.oneOf(['small', 'medium']),
}

export default EditAlbumButton
