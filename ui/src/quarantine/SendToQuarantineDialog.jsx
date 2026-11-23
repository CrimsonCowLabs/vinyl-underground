import React, { useCallback, useState } from 'react'
import PropTypes from 'prop-types'
import { useNotify, useTranslate } from 'react-admin'
import {
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
} from '@material-ui/core'
import { sendToQuarantine, skippedFiles } from './quarantine'

// The refusals the server answers with, each explained; anything else is a generic error.
const knownErrors = [
  'editingOff',
  'inQuarantine',
  'noSelection',
  'tooMany',
  'notFound',
  'changedOnDisk',
]

const quarantineErrorKey = (err) => {
  const code = err?.body?.error
  return `resources.quarantine.errors.${knownErrors.includes(code) ? code : 'unknown'}`
}

// SendToQuarantineDialog confirms, then sends the selected Tracks' Files to Quarantine as one
// job (#168): the Files move into the Quarantine folder, keeping their paths, and the Tracks
// leave the Library. Undo is in the Edit history; single Files come back from the Quarantine
// page. selection is { ids }, { albumId } or the Library manager's { filter }. onDone gets what
// the server answered: { job, background }.
export const SendToQuarantineDialog = ({
  open,
  selection,
  count,
  onClose,
  onDone,
}) => {
  const translate = useTranslate()
  const notify = useNotify()
  const [running, setRunning] = useState(false)

  const handleSend = useCallback(() => {
    setRunning(true)
    sendToQuarantine(selection)
      .then(({ job, background }) => {
        setRunning(false)
        const skipped = skippedFiles(job)
        notify(
          background
            ? 'resources.quarantine.notifications.sentInBackground'
            : skipped
              ? 'resources.quarantine.notifications.sentSkipped'
              : 'resources.quarantine.notifications.sent',
          {
            type: skipped ? 'warning' : 'info',
            messageArgs: { smart_count: skipped || count },
          },
        )
        onDone && onDone({ job, background })
      })
      .catch((err) => {
        setRunning(false)
        notify(quarantineErrorKey(err), { type: 'warning' })
      })
  }, [selection, count, notify, onDone])

  return (
    <Dialog
      open={open}
      onClose={running ? undefined : onClose}
      onClick={(e) => e.stopPropagation()}
      aria-labelledby="send-to-quarantine-title"
    >
      <DialogTitle id="send-to-quarantine-title">
        {translate('resources.quarantine.send.title', {
          smart_count: count || 0,
        })}
      </DialogTitle>
      <DialogContent>
        <DialogContentText>
          {translate('resources.quarantine.send.content', {
            smart_count: count || 0,
          })}
        </DialogContentText>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={running}>
          {translate('ra.action.cancel')}
        </Button>
        <Button
          onClick={handleSend}
          color="primary"
          disabled={running || !count}
          startIcon={running ? <CircularProgress size={16} /> : null}
        >
          {translate('resources.quarantine.send.confirm')}
        </Button>
      </DialogActions>
    </Dialog>
  )
}

SendToQuarantineDialog.propTypes = {
  open: PropTypes.bool,
  selection: PropTypes.object.isRequired,
  count: PropTypes.number,
  onClose: PropTypes.func.isRequired,
  onDone: PropTypes.func,
}

export default SendToQuarantineDialog
