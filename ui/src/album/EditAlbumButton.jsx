import React, { useCallback, useEffect, useRef, useState } from 'react'
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
  joinComposers,
  markSaved,
  sharedComposers,
  splitComposers,
  trackNumberChanges,
  trackNumbersValid,
  useTrackNumberRows,
} from './trackNumbers'
import { ReleaseLookup } from './ReleaseLookup'
import { applyOutcome, useReleaseLookup } from './useReleaseLookup'
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
  // The composers to write into every File: null until the Tracks load, then the ones they share
  const shared = sharedComposers(trackRows)
  const [composer, setComposer] = useState(null)
  useEffect(() => {
    if (shared !== null) setComposer((c) => (c === null ? shared : c))
  }, [shared])
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState(null)
  // Whether the dialog is still open: closed while saving (or its page left), the save's answer
  // only says how it went, and leaves the admin where they have gone since
  const open = useRef(true)
  useEffect(
    () => () => {
      open.current = false
    },
    [],
  )
  const suggestions = useArtistSuggestions(albumArtist)

  const yearValue = year.trim() === '' ? 0 : Number(year)
  const yearValid =
    year.trim() === '' ||
    (/^\d{4}$/.test(year.trim()) && yearValue >= 1000 && yearValue <= 9999)
  // Spaces are folded as the server folds them
  const artist = albumArtist.split(/\s+/).filter(Boolean).join(' ')
  // The release picked with Find on MusicBrainz, searched by the dialog's artist and title
  const lookup = useReleaseLookup({
    id: record.id,
    trackCount: trackRows?.length,
    artist,
    title: name,
    compilation: !!record.compilation,
  })
  const { release } = lookup
  const accepted = lookup.options.canonical
  // Only composers typed, and other than the Tracks share, are written: an empty field keeps
  // each Track's own
  const composers = splitComposers(composer || '')
  const composerChanged =
    composers.length > 0 && joinComposers(composers) !== (shared || '')
  const unchanged =
    !composerChanged &&
    name.trim() === (record.name || '') &&
    artist === (record.albumArtist || '') &&
    (yearValue === 0 || yearValue === Number(albumYear(record) || 0)) &&
    !lookup.paired
  const trackChanges = trackNumberChanges(trackRows || [])
  const canSave =
    !saving &&
    // A picked release is saved once the admin has seen how it pairs; one whose pairing
    // couldn't be had is left out, and the rest saved
    lookup.settled &&
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
      // (with a picked release, paired, written in the same job)
      const save = unchanged
        ? dataProvider
            .correctTrackNumbers(record.id, trackChanges)
            .then(({ data }) => applyOutcome(data))
        : lookup.apply({
            name: name.trim(),
            albumArtist: artist,
            year: yearValue,
            ...(composerChanged && { composer: composers }),
            ...(trackChanges.length && { tracks: trackChanges }),
          })
      save
        .then(({ done, albumId, note, notifyOptions }) => {
          notify(
            note ||
              (done
                ? 'resources.album.editor.corrected'
                : 'resources.album.editor.correcting'),
            notifyOptions,
          )
          if (!open.current) return
          setTrackRows(markSaved)
          onClose()
          if (done && (!albumId || albumId === record.id)) {
            // The album keeps its ID (e.g. only track numbers changed): its page reloads
            refresh()
          } else if (done) {
            redirect(`/album/${albumId}/show`)
          }
          // Otherwise the job finishes in the background (see the Activity panel); the page stays,
          // and says so if the album's new values take its ID with them
        })
        .catch((err) => {
          const code = err?.body?.error
          const message = translate(
            `resources.album.editor.errors.${knownErrors.includes(code) ? code : 'unknown'}`,
            { name: artist },
          )
          if (!open.current) {
            notify(message, { type: 'warning' })
            return
          }
          setError(message)
          setSaving(false)
        })
    },
    [
      dataProvider,
      record.id,
      name,
      artist,
      yearValue,
      composers,
      composerChanged,
      unchanged,
      trackChanges,
      setTrackRows,
      lookup,
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
          <TextField
            fullWidth
            margin="dense"
            label={translate('resources.album.fields.composer')}
            value={composer || ''}
            onChange={(e) => setComposer(e.target.value)}
            disabled={saving || composer === null}
            helperText={
              shared === '' &&
              trackRows.some((r) => r.composers?.length) &&
              translate('resources.album.editor.composerDiffers')
            }
            inputProps={{ 'data-testid': 'album-composer' }}
          />
          <ReleaseLookup lookup={lookup} disabled={saving} />
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
