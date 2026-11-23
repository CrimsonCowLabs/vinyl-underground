import React, { useCallback, useEffect, useMemo, useState } from 'react'
import PropTypes from 'prop-types'
import {
  useDataProvider,
  useNotify,
  useRefresh,
  useTranslate,
} from 'react-admin'
import {
  Button,
  Checkbox,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  FormControlLabel,
  IconButton,
  TextField,
  Typography,
} from '@material-ui/core'
import { Alert } from '@material-ui/lab'
import AddIcon from '@material-ui/icons/Add'
import CloseIcon from '@material-ui/icons/Close'
import { changedValues, editableValues } from './tagEdit'

// The server answers a refused edit with a code; anything else is a generic error.
const knownErrors = [
  'changedOnDisk',
  'editingOff',
  'invalid',
  'notFound',
  'unsupportedFormat',
  'notVerified',
  'linked',
  'oldCopyLeft',
  'interrupted',
]

const errorKey = (err) => {
  const code = err?.body?.error
  return `resources.song.tagEditor.errors.${knownErrors.includes(code) ? code : 'unknown'}`
}

const inputMode = { number: 'numeric', date: 'numeric' }

// FieldInput edits one field: one input per value, with add and remove for a multi-valued one.
export const FieldInput = ({ field, values, onChange, disabled }) => {
  const translate = useTranslate()
  const label = translate(`resources.song.tagEditor.fields.${field.key}`)
  const set = (i, v) => onChange(values.map((old, j) => (j === i ? v : old)))

  if (field.type === 'bool') {
    return (
      <FormControlLabel
        control={
          <Checkbox
            checked={values[0] === '1'}
            onChange={(e) => onChange([e.target.checked ? '1' : ''])}
            disabled={disabled}
            inputProps={{ 'data-testid': `tag-${field.key}-0` }}
          />
        }
        label={label}
      />
    )
  }
  const long = field.type === 'longtext'
  return (
    <div>
      {values.map((v, i) => (
        <div key={i} style={{ display: 'flex', alignItems: 'flex-end' }}>
          <TextField
            fullWidth
            margin="dense"
            label={i === 0 ? label : undefined}
            value={v}
            onChange={(e) => set(i, e.target.value)}
            disabled={disabled}
            multiline={long}
            minRows={long ? 3 : undefined}
            maxRows={long ? 12 : undefined}
            inputProps={{
              'data-testid': `tag-${field.key}-${i}`,
              inputMode: inputMode[field.type],
              'aria-label': label,
            }}
          />
          {/* A File may hold several values of a single-valued tag too: any can be removed. */}
          {values.length > 1 && (
            <IconButton
              size="small"
              onClick={() => onChange(values.filter((_, j) => j !== i))}
              disabled={disabled}
              aria-label={translate('resources.song.tagEditor.remove')}
              data-testid={`tag-${field.key}-remove-${i}`}
            >
              <CloseIcon fontSize="small" />
            </IconButton>
          )}
        </div>
      ))}
      {field.multi && (
        <Button
          size="small"
          startIcon={<AddIcon />}
          onClick={() => onChange([...values, ''])}
          disabled={disabled}
          data-testid={`tag-${field.key}-add`}
        >
          {translate('resources.song.tagEditor.add', { name: label })}
        </Button>
      )}
    </div>
  )
}

FieldInput.propTypes = {
  field: PropTypes.object.isRequired,
  values: PropTypes.arrayOf(PropTypes.string).isRequired,
  onChange: PropTypes.func.isRequired,
  disabled: PropTypes.bool,
}

// The per-Track Edit dialog (admin only, while editing is on): edits any tag the scanner maps,
// plus lyrics, as the Track's File holds them. Only the fields changed are written into the
// File, which the server checks before it replaces the File (#160). The save runs as an edit
// job, which the server waits for a little (#162): a longer one carries on in the background.
export const EditTagsDialog = ({ record, onClose }) => {
  const translate = useTranslate()
  const dataProvider = useDataProvider()
  const notify = useNotify()
  const refresh = useRefresh()
  const id = record.mediaFileId || record.id
  const [tags, setTags] = useState(null)
  const [values, setValues] = useState({})
  const [error, setError] = useState(null)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    let active = true
    dataProvider
      .getTrackTags(id)
      .then(({ data }) => {
        if (!active) return
        setTags(data)
        setValues(editableValues(data.fields, data.values))
      })
      .catch((err) => active && setError(errorKey(err)))
    return () => {
      active = false
    }
  }, [dataProvider, id])

  const changes = useMemo(
    () => (tags ? changedValues(tags.fields, tags.values, values) : {}),
    [tags, values],
  )
  const changed = Object.keys(changes).length > 0

  const handleSave = useCallback(
    (e) => {
      e.preventDefault()
      if (!changed) return
      setSaving(true)
      setError(null)
      dataProvider
        .editTrackTags(id, changes)
        .then(({ data }) => {
          if (data?.background) {
            // The edit's job takes longer than the server waits: it carries on in the
            // background, and the Activity panel shows how it goes.
            notify('resources.song.tagEditor.savingInBackground', {
              type: 'info',
            })
            onClose()
            return
          }
          const note =
            data?.changed === false
              ? 'nothingChanged'
              : data?.warning
                ? 'savedRescanPending'
                : 'saved'
          notify(`resources.song.tagEditor.${note}`, {
            type: note === 'savedRescanPending' ? 'warning' : 'info',
          })
          refresh()
          onClose()
        })
        .catch((err) => {
          setError(errorKey(err))
          setSaving(false)
        })
    },
    [changed, changes, dataProvider, id, notify, refresh, onClose],
  )

  return (
    // Clicks in the dialog would otherwise reach the Track's row (and play it).
    <span onClick={(e) => e.stopPropagation()}>
      <Dialog
        open
        onClose={saving ? undefined : onClose}
        fullWidth
        maxWidth="sm"
        aria-labelledby="edit-tags-title"
      >
        <form onSubmit={handleSave}>
          <DialogTitle id="edit-tags-title">
            {translate('resources.song.tagEditor.title')}
          </DialogTitle>
          <DialogContent>
            <DialogContentText>
              {translate('resources.song.tagEditor.help')}
            </DialogContentText>
            {tags && (
              <Typography variant="caption" color="textSecondary">
                {tags.path}
              </Typography>
            )}
            {!tags && !error && <CircularProgress size={24} />}
            {tags?.fields.map((f) => (
              <FieldInput
                key={f.key}
                field={f}
                values={values[f.key] || ['']}
                onChange={(vs) => setValues((old) => ({ ...old, [f.key]: vs }))}
                disabled={saving}
              />
            ))}
            {error && (
              <Alert severity="error" style={{ marginTop: 16 }}>
                {translate(error)}
              </Alert>
            )}
          </DialogContent>
          <DialogActions>
            <Button onClick={onClose} disabled={saving}>
              {translate('ra.action.cancel')}
            </Button>
            <Button
              type="submit"
              color="primary"
              variant="contained"
              disabled={!changed || saving}
              startIcon={saving ? <CircularProgress size={16} /> : null}
            >
              {translate('ra.action.save')}
            </Button>
          </DialogActions>
        </form>
      </Dialog>
    </span>
  )
}

EditTagsDialog.propTypes = {
  record: PropTypes.object.isRequired,
  onClose: PropTypes.func.isRequired,
}

export default EditTagsDialog
