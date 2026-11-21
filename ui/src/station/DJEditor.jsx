import React, { useEffect, useState } from 'react'
import { useNotify, useTranslate } from 'react-admin'
import {
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  TextField,
} from '@material-ui/core'
import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'
import { stationErrorKey } from './stationErrors'

// The longest DJ name and personality description the server accepts.
export const MAX_DJ_NAME_LENGTH = 60
export const MAX_DJ_DESCRIPTION_LENGTH = 1000

// saveDJ creates the Listener's DJ, or edits it when it has an id.
const saveDJ = (id, name, description) =>
  httpClient(id ? `${REST_URL}/station/dj/${id}` : `${REST_URL}/station/dj`, {
    method: id ? 'PUT' : 'POST',
    body: JSON.stringify({ name, description }),
  }).then(({ json }) => json)

// DJEditor creates one of the Listener's own DJs (dj null) or edits one, from
// a name and a personality description. onSaved gets the saved DJ.
const DJEditor = ({ open, dj, onClose, onSaved }) => {
  const translate = useTranslate()
  const notify = useNotify()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (open) {
      setName(dj?.name || '')
      setDescription(dj?.description || '')
    }
  }, [open, dj])

  const valid = name.trim() !== '' && description.trim() !== ''

  const save = (e) => {
    e.preventDefault()
    if (!valid) return
    setSaving(true)
    saveDJ(dj?.id, name.trim(), description.trim())
      .then((saved) => {
        setSaving(false)
        onSaved(saved)
      })
      .catch((err) => {
        setSaving(false)
        notify(stationErrorKey(err), { type: 'warning' })
      })
  }

  return (
    <Dialog
      open={open}
      onClose={onClose}
      aria-labelledby="dj-editor-title"
      fullWidth
      maxWidth="sm"
    >
      <form onSubmit={save}>
        <DialogTitle id="dj-editor-title">
          {translate(
            dj ? 'resources.station.dj.edit' : 'resources.station.dj.new',
          )}
        </DialogTitle>
        <DialogContent>
          <TextField
            id="dj-name"
            label={translate('resources.station.dj.fields.name')}
            value={name}
            onChange={(e) => setName(e.target.value)}
            inputProps={{ maxLength: MAX_DJ_NAME_LENGTH }}
            margin="normal"
            autoFocus
            required
            fullWidth
          />
          <TextField
            id="dj-description"
            label={translate('resources.station.dj.fields.description')}
            helperText={translate(
              'resources.station.dj.fields.descriptionHelp',
            )}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            inputProps={{ maxLength: MAX_DJ_DESCRIPTION_LENGTH }}
            margin="normal"
            minRows={3}
            multiline
            required
            fullWidth
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={onClose}>{translate('ra.action.cancel')}</Button>
          <Button type="submit" color="primary" disabled={!valid || saving}>
            {translate('ra.action.save')}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  )
}

export default DJEditor
