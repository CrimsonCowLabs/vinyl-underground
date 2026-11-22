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
import EditIcon from '@material-ui/icons/Edit'

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

  const handleSave = useCallback(
    (e) => {
      e.preventDefault()
      setSaving(true)
      setError(null)
      dataProvider
        .renameArtist(record.id, name)
        .then(({ data }) => {
          onClose()
          if (data.done) {
            notify('resources.artist.notifications.renamed', { type: 'info' })
            redirect(`/artist/${data.artistId}/show`)
          } else {
            // A large rename finishes in the background; the page updates when the scan ends
            notify('resources.artist.notifications.renaming', { type: 'info' })
          }
        })
        .catch((err) => {
          const code = err?.body?.code
          setError(
            translate(
              `resources.correction.errors.${knownErrors.includes(code) ? code : 'unknown'}`,
              { name: name.trim() },
            ),
          )
          setSaving(false)
        })
    },
    [dataProvider, record.id, name, onClose, notify, redirect, translate],
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
          <TextField
            autoFocus
            fullWidth
            label={translate('resources.artist.fields.name')}
            value={name}
            onChange={(e) => setName(e.target.value)}
            error={!!error}
            helperText={error}
            disabled={saving}
          />
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
            {translate('ra.action.save')}
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
// applies to every File with that Artist and rescans their folders.
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
