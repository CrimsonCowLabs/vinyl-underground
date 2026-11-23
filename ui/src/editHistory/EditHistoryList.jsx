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
  'alreadyUndone',
  'changedLater',
  'tagsChanged',
  'unfinished',
  'changedOnDisk',
  'editingOff',
  'notFound',
  'notUndoable',
  'notVerified',
  'unsupportedFormat',
  'linked',
  'oldCopyLeft',
  'interrupted',
  'jobUnfinished',
  'sameFileTwice',
  'coverChanged',
  'pathTaken',
  'caseTwin',
  'outside',
]

// What a cover write (the folder's cover.jpg) or an embed (Embed in Files) did, by whether it
// replaced an image, and wrote one (#164).
const imageChange = (record) => {
  const had = !!record.originalImage?.sum
  const wrote = !!record.image?.sum
  if (record.kind === 'cover') {
    return wrote ? (had ? 'coverReplaced' : 'coverWritten') : 'coverRemoved'
  }
  return wrote ? (had ? 'embedReplaced' : 'embedAdded') : 'embedRemoved'
}

// errorKey is the message of a refused undo.
const errorKey = (err) => {
  const code = err?.body?.error
  return `resources.editHistory.errors.${knownErrors.includes(code) ? code : 'unknown'}`
}

const Values = ({ values }) => {
  const classes = useStyles()
  const translate = useTranslate()
  const shown = (values || []).filter((v) => v !== '')
  if (!shown.length) {
    return (
      <span className={classes.none}>
        {translate('resources.editHistory.message.none')}
      </span>
    )
  }
  return <span>{shown.join('; ')}</span>
}

Values.propTypes = { values: PropTypes.arrayOf(PropTypes.string) }

// Each tag the write changed, "Title: Song → Song (Live)". A tag is named as the Edit dialog
// names its field (TagLib's TITLE is the dialog's "title"), or by its own name otherwise.
export const EditChangesField = ({ record }) => {
  const classes = useStyles()
  const translate = useTranslate()
  if (!record) return null
  if (record.kind === 'cover' || record.kind === 'embed') {
    return (
      <Typography variant="body2" component="span" className={classes.change}>
        {translate(`resources.editHistory.message.${imageChange(record)}`)}
      </Typography>
    )
  }
  if (record.kind === 'move') {
    // A move (Organize, #167): where the File was, and where it went.
    return (
      <Typography variant="body2" component="span" className={classes.change}>
        <span className={classes.tag}>
          {translate('resources.editHistory.message.moved')}
        </span>
        <span>{record.path}</span>
        <span className={classes.arrow} aria-hidden>
          →
        </span>
        <span>{record.newPath}</span>
      </Typography>
    )
  }
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
  if (record.kind === 'cover') {
    // A folder cover is no Track's.
    return (
      <Typography variant="body2" component="span" className={classes.none}>
        {translate('resources.editHistory.message.albumCover')}
      </Typography>
    )
  }
  if (!record.track && record.kind === 'move' && !record.mediaFileId) {
    // A File moved with its album (its cover.jpg, ...) is no Track's.
    return (
      <Typography variant="body2" component="span" className={classes.none}>
        {translate('resources.editHistory.message.albumFile')}
      </Typography>
    )
  }
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
      {record.track.missing && (
        <span className={classes.none}>
          {translate('resources.editHistory.message.trackMissing')}
        </span>
      )}
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
        label={translate(
          `resources.editHistory.status.${record.redo ? 'redo' : 'undo'}`,
        )}
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
          data?.background
            ? 'resources.editHistory.notifications.undoInBackground'
            : data?.warning
              ? 'resources.editHistory.notifications.undoneRescanPending'
              : record.kind === 'cover'
                ? 'resources.editHistory.notifications.undoneCover'
                : 'resources.editHistory.notifications.undone',
          { type: 'info' },
        ),
      )
      .catch((err) => notify(errorKey(err), { type: 'warning' }))
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

// An entry that is part of a job of several Files (#162) is marked, and the whole job can be
// undone from it: every File it wrote, newest first, as a job of its own. A File the undo can't
// put back (a later write changed it, ...) is skipped; the Activity panel lists why. A job of a
// kind that does more than write its Files (it moves stars, or removes Corrections) is undone
// whole even when it wrote one File, so that is undone too.
const jobKindsUndoneWhole = new Set([
  'albumEdit',
  'trackNumbers',
  'artistRename',
  'artistMerge',
  'migrateCorrections',
  // Organize (#167): its undo also removes the folders it made, and rescans.
  'organize',
])

export const EditJobField = ({ record }) => {
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
      .undoEditJob(record.jobId)
      .then(({ data }) => {
        if (data?.state !== 'done') {
          notify('resources.editHistory.notifications.undoInBackground', {
            type: 'info',
          })
        } else if (data.skipped > 0) {
          notify('resources.editHistory.notifications.undoneJobSkipped', {
            type: 'warning',
            messageArgs: { smart_count: data.skipped },
          })
        } else {
          notify('resources.editHistory.notifications.undoneJob', {
            type: 'info',
            messageArgs: { smart_count: data.total },
          })
        }
      })
      .catch((err) => notify(errorKey(err), { type: 'warning' }))
      .finally(() => {
        setRunning(false)
        refresh()
      })
  }, [dataProvider, record, notify, refresh])

  if (
    !record ||
    !(record.jobSize > 1 || jobKindsUndoneWhole.has(record.jobKind))
  )
    return null
  return (
    <>
      <Chip
        size="small"
        variant="outlined"
        label={translate('resources.editHistory.message.job', {
          smart_count: record.jobSize,
        })}
      />
      <Button
        label={translate('resources.editHistory.actions.undoJob')}
        onClick={(e) => {
          e.stopPropagation()
          setOpen(true)
        }}
        disabled={running}
      >
        <UndoIcon />
      </Button>
      <Confirm
        isOpen={open}
        title="resources.editHistory.message.undoJobTitle"
        content="resources.editHistory.message.undoJobContent"
        onConfirm={handleConfirm}
        onClose={() => setOpen(false)}
      />
    </>
  )
}

EditJobField.propTypes = { record: PropTypes.object }

const HistoryPagination = (props) => (
  <Pagination rowsPerPageOptions={[25, 50, 100]} {...props} />
)

// Every write the app made to a File, newest first (the server's order; it isn't sortable). A
// job's writes are next to each other, as jobs run one at a time, and each is marked as part of
// its job (see EditJobField).
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
        <FunctionField
          source="jobId"
          sortable={false}
          render={(r) => <EditJobField record={r} />}
        />
      </Datagrid>
    </List>
  )
}

export default EditHistoryList
