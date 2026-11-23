import React, { useCallback, useState } from 'react'
import PropTypes from 'prop-types'
import {
  Button,
  Confirm,
  Datagrid,
  DateField,
  FunctionField,
  List,
  Pagination,
  TextField,
  useDataProvider,
  useNotify,
  useRefresh,
  useTranslate,
} from 'react-admin'
import { Chip, Tooltip, Typography } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import UndoIcon from '@material-ui/icons/Undo'

const useStyles = makeStyles((theme) => ({
  change: { display: 'block' },
  tag: { fontWeight: 500, marginRight: theme.spacing(1) },
  arrow: { margin: theme.spacing(0, 1), color: theme.palette.text.secondary },
  none: { fontStyle: 'italic', color: theme.palette.text.secondary },
  secondary: { display: 'block', color: theme.palette.text.secondary },
}))

// The refusals an undo can answer with, each explained on its own.
const knownErrors = [
  'changedLater',
  'changedOnDisk',
  'editingOff',
  'notFound',
  'notUndoable',
  'notVerified',
  'unsupportedFormat',
  'linked',
  'oldCopyLeft',
]

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

// Each tag the write changed, "Title: Song → Song (Live)". A tag is named as the Edit dialog
// names its field (TagLib's TITLE is the dialog's "title"), or by its own name otherwise.
export const EditChangesField = ({ record }) => {
  const classes = useStyles()
  const translate = useTranslate()
  if (!record) return null
  return (
    <>
      {(record.changes || []).map((c) => (
        <Typography
          key={c.tag}
          variant="body2"
          component="span"
          className={classes.change}
        >
          <span className={classes.tag}>
            {translate(
              `resources.song.tagEditor.fields.${c.tag.toLowerCase()}`,
              {
                _: c.tag,
              },
            )}
          </span>
          <Values values={c.old} />
          <span className={classes.arrow} aria-hidden>
            →
          </span>
          <Values values={c.new} />
        </Typography>
      ))}
    </>
  )
}

EditChangesField.propTypes = { record: PropTypes.object }

// The Track the write was to, as the app has it now.
export const EditTrackField = ({ record }) => {
  const classes = useStyles()
  const translate = useTranslate()
  if (!record) return null
  if (!record.track) {
    return (
      <Typography variant="body2" component="span" className={classes.none}>
        {translate('resources.editHistory.message.trackGone')}
      </Typography>
    )
  }
  return (
    <Typography variant="body2" component="span">
      <span>{record.track.title}</span>
      <span className={classes.secondary}>{record.track.artist}</span>
    </Typography>
  )
}

EditTrackField.propTypes = { record: PropTypes.object }

// An undo, and a write that didn't happen (or hasn't finished), are marked.
export const EditStatusField = ({ record }) => {
  const translate = useTranslate()
  if (!record) return null
  if (record.state === 'failed' || record.state === 'pending') {
    const chip = (
      <Chip
        size="small"
        color="secondary"
        label={translate(`resources.editHistory.status.${record.state}`)}
      />
    )
    return record.error ? <Tooltip title={record.error}>{chip}</Tooltip> : chip
  }
  if (record.undoOf) {
    return (
      <Chip
        size="small"
        label={translate('resources.editHistory.status.undo')}
      />
    )
  }
  return null
}

EditStatusField.propTypes = { record: PropTypes.object }

// Undo writes the entry's original values back into the File, as an edit does; the undo is in
// the list too, and can be undone itself (a redo). The server refuses one that would clobber
// newer values.
export const UndoEditButton = ({ record }) => {
  const translate = useTranslate()
  const dataProvider = useDataProvider()
  const notify = useNotify()
  const refresh = useRefresh()
  const [open, setOpen] = useState(false)
  const [running, setRunning] = useState(false)

  const handleConfirm = useCallback(() => {
    setOpen(false)
    setRunning(true)
    dataProvider
      .undoEdit(record.id)
      .then(({ data }) =>
        notify(
          data?.warning
            ? 'resources.editHistory.notifications.undoneRescanPending'
            : 'resources.editHistory.notifications.undone',
          { type: 'info' },
        ),
      )
      .catch((err) => {
        const code = err?.body?.error
        notify(
          `resources.editHistory.errors.${knownErrors.includes(code) ? code : 'unknown'}`,
          { type: 'warning' },
        )
      })
      .finally(() => {
        setRunning(false)
        refresh()
      })
  }, [dataProvider, record, notify, refresh])

  if (!record) return null
  return (
    <>
      <Button
        label={translate('resources.editHistory.actions.undo')}
        onClick={(e) => {
          e.stopPropagation()
          setOpen(true)
        }}
        disabled={running || record.state !== 'written'}
      >
        <UndoIcon />
      </Button>
      <Confirm
        isOpen={open}
        title="resources.editHistory.message.undoTitle"
        content="resources.editHistory.message.undoContent"
        onConfirm={handleConfirm}
        onClose={() => setOpen(false)}
      />
    </>
  )
}

UndoEditButton.propTypes = { record: PropTypes.object }

const HistoryPagination = (props) => (
  <Pagination rowsPerPageOptions={[25, 50, 100]} {...props} />
)

// Every write the app made to a File, newest first (the server's order; it isn't sortable).
const EditHistoryList = (props) => {
  return (
    <List
      {...props}
      exporter={false}
      bulkActionButtons={false}
      perPage={25}
      pagination={<HistoryPagination />}
    >
      <Datagrid>
        <DateField source="createdAt" showTime sortable={false} />
        <TextField source="path" sortable={false} />
        <FunctionField
          source="track"
          sortable={false}
          render={(r) => <EditTrackField record={r} />}
        />
        <FunctionField
          source="changes"
          sortable={false}
          render={(r) => <EditChangesField record={r} />}
        />
        <FunctionField
          source="state"
          sortable={false}
          render={(r) => <EditStatusField record={r} />}
        />
        <UndoEditButton />
      </Datagrid>
    </List>
  )
}

export default EditHistoryList
