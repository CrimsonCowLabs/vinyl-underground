import React, { useCallback, useEffect, useState } from 'react'
import PropTypes from 'prop-types'
import { useNotify, useTranslate } from 'react-admin'
import {
  Button,
  Checkbox,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  FormControl,
  FormControlLabel,
  InputLabel,
  MenuItem,
  Select,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Typography,
} from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import { FieldInput } from '../song/EditTagsDialog'
import {
  bulkEditRequest,
  getBulkFields,
  previewBulkEdit,
  runBulkEdit,
} from './bulkEdit'

const useStyles = makeStyles((theme) => ({
  field: { minWidth: 240, marginBottom: theme.spacing(1) },
  none: { fontStyle: 'italic', color: theme.palette.text.secondary },
  arrow: { margin: theme.spacing(0, 1), color: theme.palette.text.secondary },
  path: { color: theme.palette.text.secondary, wordBreak: 'break-all' },
  refused: { color: theme.palette.text.secondary },
}))

// The rows of the preview shown: the counts cover every Track.
const previewRows = 100

// The refusals the server answers with, each explained; anything else is a generic error.
const knownErrors = [
  'editingOff',
  'invalid',
  'noSelection',
  'tooMany',
  'nothingToChange',
]

const errorKey = (err) => {
  const code = err?.body?.error
  return `resources.libraryTrack.bulkEdit.errors.${knownErrors.includes(code) ? code : 'unknown'}`
}

const Values = ({ values }) => {
  const classes = useStyles()
  const translate = useTranslate()
  if (!values?.length) {
    return (
      <span className={classes.none}>
        {translate('resources.editHistory.message.none')}
      </span>
    )
  }
  return <span>{values.join('; ')}</span>
}

Values.propTypes = { values: PropTypes.arrayOf(PropTypes.string) }

// What the edit does to a Track: its change, or why its File is left alone.
const statusKey = (status) =>
  status === 'change'
    ? null
    : status === 'unchanged'
      ? 'resources.libraryTrack.bulkEdit.unchanged'
      : `resources.song.tagEditor.errors.${status}`

const Preview = ({ preview }) => {
  const classes = useStyles()
  const translate = useTranslate()
  const refused = Object.values(preview.refused || {}).reduce(
    (a, b) => a + b,
    0,
  )
  return (
    <>
      <DialogContentText data-testid="bulk-edit-summary">
        {translate('resources.libraryTrack.bulkEdit.summary', {
          smart_count: preview.changes,
          total: preview.total,
          unchanged: preview.unchanged,
          refused,
        })}
      </DialogContentText>
      <Table size="small">
        <TableHead>
          <TableRow>
            <TableCell>
              {translate('resources.libraryTrack.fields.title')}
            </TableCell>
            <TableCell>
              {translate('resources.libraryTrack.bulkEdit.change')}
            </TableCell>
          </TableRow>
        </TableHead>
        <TableBody>
          {preview.rows.map((r) => (
            <TableRow key={r.id} data-testid={`bulk-edit-row-${r.id}`}>
              <TableCell>
                <div>{r.title}</div>
                <Typography variant="caption" className={classes.path}>
                  {r.path}
                </Typography>
              </TableCell>
              <TableCell>
                {r.status === 'change' ? (
                  <>
                    <Values values={r.old} />
                    <span className={classes.arrow} aria-hidden>
                      →
                    </span>
                    <Values values={r.new} />
                  </>
                ) : (
                  <span className={classes.refused}>
                    {translate(statusKey(r.status), {
                      _: translate('resources.song.tagEditor.errors.unknown'),
                    })}
                  </span>
                )}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {preview.total > preview.rows.length && (
        <Typography variant="caption">
          {translate('resources.libraryTrack.bulkEdit.more', {
            smart_count: preview.total - preview.rows.length,
          })}
        </Typography>
      )}
    </>
  )
}

Preview.propTypes = { preview: PropTypes.object.isRequired }

// The bulk edit (#166): set one field, or clear it, across the selected Tracks or all matching
// the filter. It previews each Track's old and new value, then runs as one edit job, which the
// Edit history lists and can undo as a whole. The server waits a little for the job; a longer
// one runs on in the background, and the Activity panel follows it.
export const BulkEditDialog = ({ open, selection, count, onClose, onDone }) => {
  const classes = useStyles()
  const translate = useTranslate()
  const notify = useNotify()
  const [fields, setFields] = useState(null)
  const [key, setKey] = useState('')
  const [values, setValues] = useState([''])
  const [clear, setClear] = useState(false)
  const [preview, setPreview] = useState(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!open) return
    setKey('')
    setValues([''])
    setClear(false)
    setPreview(null)
    getBulkFields()
      .then(setFields)
      .catch((err) => notify(errorKey(err), { type: 'warning' }))
  }, [open, notify])

  const field = (fields || []).find((f) => f.key === key)
  const request = () =>
    bulkEditRequest({ selection, field: key, values, clear })

  const handlePreview = useCallback(() => {
    setBusy(true)
    previewBulkEdit(request(), 0, previewRows)
      .then(setPreview)
      .catch((err) => notify(errorKey(err), { type: 'warning' }))
      .finally(() => setBusy(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selection, key, values, clear, notify])

  const handleRun = useCallback(() => {
    setBusy(true)
    runBulkEdit(request())
      .then(({ job, background }) => {
        if (background) {
          notify('resources.libraryTrack.bulkEdit.inBackground', {
            type: 'info',
          })
        } else if (job.skipped > 0) {
          notify('resources.libraryTrack.bulkEdit.doneSkipped', {
            type: 'warning',
            messageArgs: { smart_count: job.skipped },
          })
        } else {
          notify('resources.libraryTrack.bulkEdit.done', {
            type: 'info',
            messageArgs: { smart_count: job.done },
          })
        }
        onDone()
      })
      .catch((err) => notify(errorKey(err), { type: 'warning' }))
      .finally(() => setBusy(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selection, key, values, clear, notify, onDone])

  const changeField = (e) => {
    setKey(e.target.value)
    setValues([''])
    setPreview(null)
  }

  return (
    <Dialog
      open={open}
      onClose={busy ? undefined : onClose}
      maxWidth="md"
      fullWidth
      aria-labelledby="bulk-edit-title"
    >
      <DialogTitle id="bulk-edit-title">
        {translate(
          selection.filter
            ? 'resources.libraryTrack.bulkEdit.titleMatching'
            : 'resources.libraryTrack.bulkEdit.titleSelected',
          { smart_count: count },
        )}
      </DialogTitle>
      <DialogContent>
        {!fields ? (
          <CircularProgress size={24} />
        ) : preview ? (
          <Preview preview={preview} />
        ) : (
          <>
            <DialogContentText>
              {translate('resources.libraryTrack.bulkEdit.help')}
            </DialogContentText>
            <FormControl className={classes.field}>
              <InputLabel id="bulk-edit-field">
                {translate('resources.libraryTrack.bulkEdit.field')}
              </InputLabel>
              <Select
                labelId="bulk-edit-field"
                value={key}
                onChange={changeField}
                inputProps={{ 'data-testid': 'bulk-edit-field' }}
              >
                {fields.map((f) => (
                  <MenuItem key={f.key} value={f.key}>
                    {translate(`resources.song.tagEditor.fields.${f.key}`)}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
            {field && (
              <>
                {!clear && (
                  <FieldInput
                    field={field}
                    values={values}
                    onChange={setValues}
                    disabled={busy}
                  />
                )}
                <FormControlLabel
                  control={
                    <Checkbox
                      checked={clear}
                      onChange={(e) => setClear(e.target.checked)}
                      inputProps={{ 'data-testid': 'bulk-edit-clear' }}
                    />
                  }
                  label={translate('resources.libraryTrack.bulkEdit.clear')}
                />
              </>
            )}
          </>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={busy}>
          {translate('ra.action.cancel')}
        </Button>
        {preview ? (
          <>
            <Button onClick={() => setPreview(null)} disabled={busy}>
              {translate('ra.action.back')}
            </Button>
            <Button
              color="primary"
              variant="contained"
              onClick={handleRun}
              disabled={busy || preview.changes === 0}
              data-testid="bulk-edit-run"
            >
              {translate('resources.libraryTrack.bulkEdit.run', {
                smart_count: preview.changes,
              })}
            </Button>
          </>
        ) : (
          <Button
            color="primary"
            variant="contained"
            onClick={handlePreview}
            disabled={busy || !field}
            data-testid="bulk-edit-preview"
          >
            {translate('resources.libraryTrack.bulkEdit.preview')}
          </Button>
        )}
      </DialogActions>
    </Dialog>
  )
}

BulkEditDialog.propTypes = {
  open: PropTypes.bool.isRequired,
  // { ids } for the selected Tracks, or { filter } for all matching it.
  selection: PropTypes.object.isRequired,
  count: PropTypes.number,
  onClose: PropTypes.func.isRequired,
  onDone: PropTypes.func.isRequired,
}

export default BulkEditDialog
