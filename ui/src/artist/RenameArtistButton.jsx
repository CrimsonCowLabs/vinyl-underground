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
import { nameKey, useArtistSuggestions } from './useArtistSuggestions'

// The server answers a refused Correction with a code; anything else is a generic error.
const knownErrors = [
  'artistExists',
  'renamedAway',
  'alreadyCorrected',
  'invalid',
  'notFound',
]

const RenameArtistDialog = ({ record, onClose }) => {
  const translate = useTranslate()
  const dataProvider = useDataProvider()
  const notify = useNotify()
  const redirect = useRedirect()
  const [name, setName] = useState(record.name)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState(null)
  const suggestions = useArtistSuggestions(name, record.id)
  // Renaming to another Artist's name merges into that Artist
  const target = suggestions.find((a) => nameKey(a.name) === nameKey(name))

  const handleSave = useCallback(
    (e) => {
      e.preventDefault()
      setSaving(true)
      setError(null)
      const save = target
        ? dataProvider.mergeArtist(record.id, target.id)
        : dataProvider.renameArtist(record.id, name)
      const notifications = target
        ? { done: 'merged', background: 'merging' }
        : { done: 'renamed', background: 'renaming' }
      save
        .then(({ data }) => {
          onClose()
          if (data.done) {
            notify(`resources.artist.notifications.${notifications.done}`, {
              type: 'info',
            })
            redirect(`/artist/${data.artistId}/show`)
          } else {
            // A large rename finishes in the background, and this page's Artist ID goes with it
            notify(
              `resources.artist.notifications.${notifications.background}`,
              {
                type: 'info',
              },
            )
            redirect('/artist')
          }
        })
        .catch((err) => {
          const code = err?.body?.error
          setError(
            translate(
              `resources.correction.errors.${knownErrors.includes(code) ? code : 'unknown'}`,
              { name: name.trim() },
            ),
          )
          setSaving(false)
        })
    },
    [
      dataProvider,
      record.id,
      name,
      target,
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
      aria-labelledby="rename-artist-title"
    >
      <form onSubmit={handleSave}>
        <DialogTitle id="rename-artist-title">
          {translate('resources.artist.actions.rename')}
        </DialogTitle>
        <DialogContent>
          <DialogContentText>
            {translate('resources.artist.message.renameHelp')}
          </DialogContentText>
          <Autocomplete
            freeSolo
            disableClearable
            options={suggestions}
            filterOptions={(options) => options}
            getOptionLabel={(a) => (typeof a === 'string' ? a : a.name)}
            value={name}
            onChange={(_, value) =>
              setName(typeof value === 'string' ? value : value?.name || '')
            }
            inputValue={name}
            onInputChange={(_, value) => setName(value)}
            disabled={saving}
            renderInput={(params) => (
              <TextField
                {...params}
                autoFocus
                fullWidth
                label={translate('resources.artist.fields.name')}
                error={!!error}
                helperText={error}
              />
            )}
          />
          {target && (
            <Alert severity="warning" style={{ marginTop: 16 }}>
              {translate('resources.artist.message.mergeWarning', {
                name: target.name,
                smart_count: target.albumCount || 0,
              })}
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
            disabled={saving || !name.trim() || name.trim() === record.name}
            startIcon={saving ? <CircularProgress size={16} /> : null}
          >
            {translate(
              target ? 'resources.artist.actions.merge' : 'ra.action.save',
            )}
          </MuiButton>
        </DialogActions>
      </form>
    </Dialog>
  )
}

RenameArtistDialog.propTypes = {
  record: PropTypes.object.isRequired,
  onClose: PropTypes.func.isRequired,
}

// "Rename" on an Artist page (admin only): saves an artist rename Correction, which the server
// applies to every File with that Artist and rescans their folders. Renaming to an existing
// Artist, offered as you type, merges into it instead, after a warning.
export const RenameArtistButton = ({ record, className, size }) => {
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
        label={translate('resources.artist.actions.rename')}
      >
        <EditIcon />
      </Button>
      {open && <RenameArtistDialog record={record} onClose={handleClose} />}
    </>
  )
}

RenameArtistButton.propTypes = {
  record: PropTypes.object,
  className: PropTypes.string,
  size: PropTypes.oneOf(['small', 'medium']),
}

export default RenameArtistButton
