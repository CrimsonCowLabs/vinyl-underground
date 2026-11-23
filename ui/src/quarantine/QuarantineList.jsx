import React, { useCallback, useEffect, useState } from 'react'
import PropTypes from 'prop-types'
import { Title, useNotify, useTranslate } from 'react-admin'
import {
  Button,
  Card,
  CardContent,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TablePagination,
  TableRow,
  Typography,
} from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import RestoreIcon from '@material-ui/icons/Restore'
import DeleteForeverIcon from '@material-ui/icons/DeleteForever'
import { useLibraryEditing } from '../library/useLibraryEditing'
import {
  emptyQuarantine,
  formatSize,
  getQuarantine,
  restoreFromQuarantine,
} from './quarantine'

const useStyles = makeStyles((theme) => ({
  header: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    flexWrap: 'wrap',
    gap: theme.spacing(1),
  },
  path: { wordBreak: 'break-all' },
  secondary: { display: 'block', color: theme.palette.text.secondary },
  warning: { color: theme.palette.error.main, fontWeight: 'bold' },
}))

// The refusals a restore or Empty Quarantine can answer with, each explained.
const knownErrors = [
  'editingOff',
  'pathTaken',
  'caseTwin',
  'outside',
  'changedOnDisk',
  'changedLater',
  'alreadyUndone',
  'unfinished',
  'deletedForGood',
  'notInQuarantine',
  'nothingInQuarantine',
  'quarantineChanged',
]

const errorKey = (err) => {
  const code = err?.body?.error
  return `resources.quarantine.errors.${knownErrors.includes(code) ? code : 'unknown'}`
}

// EmptyQuarantineDialog states what Empty Quarantine deletes, and that it can't be undone.
export const EmptyQuarantineDialog = ({
  open,
  count,
  size,
  running,
  onClose,
  onConfirm,
}) => {
  const translate = useTranslate()
  const classes = useStyles()
  return (
    <Dialog
      open={open}
      onClose={running ? undefined : onClose}
      aria-labelledby="empty-quarantine-title"
    >
      <DialogTitle id="empty-quarantine-title">
        {translate('resources.quarantine.empty.title')}
      </DialogTitle>
      <DialogContent>
        <DialogContentText>
          {translate('resources.quarantine.empty.content', {
            smart_count: count,
            size: formatSize(size),
          })}
        </DialogContentText>
        <DialogContentText className={classes.warning}>
          {translate('resources.quarantine.empty.cannotUndo')}
        </DialogContentText>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={running}>
          {translate('ra.action.cancel')}
        </Button>
        <Button
          onClick={onConfirm}
          className={classes.warning}
          disabled={running}
          startIcon={running ? <CircularProgress size={16} /> : null}
        >
          {translate('resources.quarantine.empty.confirm', {
            smart_count: count,
          })}
        </Button>
      </DialogActions>
    </Dialog>
  )
}

EmptyQuarantineDialog.propTypes = {
  open: PropTypes.bool,
  count: PropTypes.number,
  size: PropTypes.number,
  running: PropTypes.bool,
  onClose: PropTypes.func.isRequired,
  onConfirm: PropTypes.func.isRequired,
}

const FileRow = ({ file, canWrite, busy, onRestore }) => {
  const classes = useStyles()
  const translate = useTranslate()
  return (
    <TableRow>
      <TableCell className={classes.path}>
        {file.path}
        {file.library && (
          <span className={classes.secondary}>{file.library}</span>
        )}
      </TableCell>
      <TableCell className={classes.path}>{file.original}</TableCell>
      <TableCell>
        {file.trackId ? (
          <>
            {file.title}
            <span className={classes.secondary}>{file.artist}</span>
          </>
        ) : (
          <span className={classes.secondary}>
            {translate('resources.quarantine.message.noTrack')}
          </span>
        )}
      </TableCell>
      <TableCell align="right">{formatSize(file.size)}</TableCell>
      <TableCell>
        <Button
          size="small"
          startIcon={<RestoreIcon />}
          disabled={busy || !canWrite}
          onClick={() => onRestore(file)}
        >
          {translate('resources.quarantine.actions.restore')}
        </Button>
      </TableCell>
    </TableRow>
  )
}

FileRow.propTypes = {
  file: PropTypes.object.isRequired,
  canWrite: PropTypes.bool,
  busy: PropTypes.bool,
  onRestore: PropTypes.func.isRequired,
}

// The Quarantine page (#168): every File in Quarantine, where it came from and its Track, with a
// restore for each, and Empty Quarantine, the only way the app deletes Files.
const QuarantineList = () => {
  const translate = useTranslate()
  const notify = useNotify()
  const classes = useStyles()
  const editing = useLibraryEditing()
  const [contents, setContents] = useState(null)
  const [busy, setBusy] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [page, setPage] = useState(0)
  const [perPage, setPerPage] = useState(50)

  const load = useCallback(
    () =>
      getQuarantine()
        .then(setContents)
        .catch((err) => notify(errorKey(err), { type: 'warning' })),
    [notify],
  )
  useEffect(() => {
    load()
  }, [load])

  // What Empty Quarantine did: done, Files left (the step's note says why), the step itself
  // refused, or still running in the background.
  const emptied = useCallback(
    ({ job, background }) => {
      const step = job?.steps?.[0]
      if (background) {
        notify('resources.quarantine.notifications.emptyInBackground', {
          type: 'info',
        })
      } else if (step?.state === 'skipped') {
        notify(errorKey({ body: { error: step.code } }), { type: 'warning' })
      } else if (step?.warning) {
        notify('resources.quarantine.notifications.left', {
          type: 'warning',
          messageArgs: { warning: step.warning },
        })
      } else {
        notify('resources.quarantine.notifications.emptied', { type: 'info' })
      }
    },
    [notify],
  )

  const handleRestore = useCallback(
    (file) => {
      setBusy(true)
      restoreFromQuarantine(file)
        .then(({ background }) =>
          notify(
            background
              ? 'resources.quarantine.notifications.restoreInBackground'
              : 'resources.quarantine.notifications.restored',
            { type: 'info', messageArgs: { path: file.original } },
          ),
        )
        .catch((err) => notify(errorKey(err), { type: 'warning' }))
        .finally(() => {
          setBusy(false)
          load()
        })
    },
    [notify, load],
  )

  const handleEmpty = useCallback(() => {
    setBusy(true)
    emptyQuarantine({ count: contents.emptyCount, size: contents.emptySize })
      .then(emptied)
      .catch((err) => notify(errorKey(err), { type: 'warning' }))
      .finally(() => {
        setBusy(false)
        setConfirming(false)
        load()
      })
  }, [contents, emptied, notify, load])

  const files = contents?.files || []
  // A restore and Empty Quarantine need the File's library writable, as the server says.
  const canWrite = (f) => editing.enabled && f.writable === true

  // A shorter list (after a restore, or Empty) starts again at its first page.
  useEffect(() => {
    if (page > 0 && page * perPage >= files.length) setPage(0)
  }, [page, perPage, files.length])

  return (
    <Card>
      <Title
        title={translate('resources.quarantine.name', { smart_count: 2 })}
      />
      <CardContent>
        <div className={classes.header}>
          <Typography variant="body1">
            {contents
              ? translate('resources.quarantine.message.summary', {
                  smart_count: contents.count,
                  size: formatSize(contents.size),
                })
              : translate('ra.page.loading')}
          </Typography>
          <Button
            startIcon={<DeleteForeverIcon />}
            disabled={busy || !contents?.emptyCount || !editing.enabled}
            onClick={() => setConfirming(true)}
          >
            {translate('resources.quarantine.actions.empty')}
          </Button>
        </div>
        {contents && !contents.count ? (
          <Typography>
            {translate('resources.quarantine.empty.none')}
          </Typography>
        ) : (
          files.length > 0 && (
            <>
              <Table size="small">
                <TableHead>
                  <TableRow>
                    <TableCell>
                      {translate('resources.quarantine.fields.path')}
                    </TableCell>
                    <TableCell>
                      {translate('resources.quarantine.fields.original')}
                    </TableCell>
                    <TableCell>
                      {translate('resources.quarantine.fields.track')}
                    </TableCell>
                    <TableCell align="right">
                      {translate('resources.quarantine.fields.size')}
                    </TableCell>
                    <TableCell />
                  </TableRow>
                </TableHead>
                <TableBody>
                  {files
                    .slice(page * perPage, (page + 1) * perPage)
                    .map((f) => (
                      <FileRow
                        key={`${f.libraryId}:${f.path}`}
                        file={f}
                        canWrite={canWrite(f)}
                        busy={busy}
                        onRestore={handleRestore}
                      />
                    ))}
                </TableBody>
              </Table>
              <TablePagination
                component="div"
                count={files.length}
                page={page}
                rowsPerPage={perPage}
                rowsPerPageOptions={[25, 50, 100, 250]}
                onPageChange={(_, p) => setPage(p)}
                onRowsPerPageChange={(e) => {
                  setPerPage(parseInt(e.target.value, 10))
                  setPage(0)
                }}
              />
            </>
          )
        )}
      </CardContent>
      <EmptyQuarantineDialog
        open={confirming}
        count={contents?.emptyCount || 0}
        size={contents?.emptySize || 0}
        running={busy}
        onClose={() => setConfirming(false)}
        onConfirm={handleEmpty}
      />
    </Card>
  )
}

export default QuarantineList
