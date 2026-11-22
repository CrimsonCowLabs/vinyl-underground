import React, { useCallback, useState } from 'react'
import PropTypes from 'prop-types'
import {
  Button,
  useDataProvider,
  useNotify,
  usePermissions,
  useRedirect,
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
import { FindOnMusicBrainz } from './FindOnMusicBrainz'

// The server answers a refused Correction with a code; anything else is a generic error.
const knownErrors = [
  'sharedFolder',
  'renamedAway',
  'alreadyCorrected',
  'invalid',
  'notFound',
]

// The year clients show for the album: its original year when it has one, as the server
// compares it.
const albumYear = (record) => record.maxOriginalYear || record.maxYear || ''

// The album Edit dialog: corrects the album's title, album artist (offered from the existing
// Artists as you type) and year. Only what is changed is corrected.
const EditAlbumDialog = ({ record, onClose }) => {
  const translate = useTranslate()
  const dataProvider = useDataProvider()
  const notify = useNotify()
  const redirect = useRedirect()
  const [name, setName] = useState(record.name || '')
  const [albumArtist, setAlbumArtist] = useState(record.albumArtist || '')
  const [year, setYear] = useState(String(albumYear(record)))
  const [release, setRelease] = useState(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState(null)
  const suggestions = useArtistSuggestions(albumArtist)

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
    !release
  const canSave = !saving && name.trim() && artist && yearValid && !unchanged

  const handleSave = useCallback(
    (e) => {
      e.preventDefault()
      setSaving(true)
      setError(null)
      dataProvider
        .correctAlbum(record.id, {
          name: name.trim(),
          albumArtist: artist,
          year: yearValue,
          ...(release && {
            mbzAlbumId: release.id,
            mbzReleaseGroupId: release.releaseGroupId,
            mbzRelease: release.description,
          }),
        })
        .then(({ data }) => {
          onClose()
          if (data.done) {
            notify('resources.album.editor.corrected', { type: 'info' })
            redirect(`/album/${data.albumId}/show`)
          } else {
            // The rescan finishes in the background, and this page's album ID goes with it
            notify('resources.album.editor.correcting', { type: 'info' })
            redirect('/album')
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
      release,
      onClose,
      notify,
      redirect,
      translate,
    ],
  )

  return (
    <Dialog
      open
      onClose={onClose}
      fullWidth
      maxWidth="xs"
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
            disabled={saving}
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
            disabled={saving}
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
            disabled={saving}
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

// "Edit" on an album page (admin only): saves an album Correction of its title, album artist
// and year, which the server applies to the album's folder and rescans it.
export const EditAlbumButton = ({ record, className, size }) => {
  const translate = useTranslate()
  const { permissions } = usePermissions()
  const [open, setOpen] = useState(false)
  const handleClose = useCallback(() => setOpen(false), [])

  if (permissions !== 'admin' || !record?.id) return null

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
