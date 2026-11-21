import React, { useState } from 'react'
import { useNotify, useRefresh, useTranslate } from 'react-admin'
import {
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Typography,
} from '@material-ui/core'
import { httpClient } from '../../dataProvider'
import { errorText, jobUrl } from './reviewApi'

// DiscardDialog asks before deleting Files for good: they don't go to Quarantine.
export const DiscardDialog = ({ open, message, busy, onConfirm, onCancel }) => {
  const translate = useTranslate()
  return (
    <Dialog open={open} onClose={onCancel}>
      <DialogTitle>
        {translate('resources.rip.review.discardTitle')}
      </DialogTitle>
      <DialogContent>
        <Typography>{message}</Typography>
      </DialogContent>
      <DialogActions>
        <Button onClick={onCancel}>{translate('ra.action.cancel')}</Button>
        <Button
          onClick={onConfirm}
          variant="contained"
          color="secondary"
          disabled={busy}
        >
          {translate('resources.rip.review.discard')}
        </Button>
      </DialogActions>
    </Dialog>
  )
}

// DiscardJobButton discards a whole job, once confirmed: all its Files, and the job itself.
// fileCount is how many Files the job has, for the confirmation.
export const DiscardJobButton = ({ jobId, fileCount }) => {
  const translate = useTranslate()
  const notify = useNotify()
  const refresh = useRefresh()
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)

  const handleConfirm = () => {
    setBusy(true)
    httpClient(jobUrl(jobId), { method: 'DELETE' })
      .then(() => {
        notify('resources.rip.review.discarded', 'info')
        setOpen(false)
        refresh()
      })
      .catch((error) => notify(errorText(error), 'warning'))
      .finally(() => setBusy(false))
  }

  return (
    <>
      <Button color="secondary" onClick={() => setOpen(true)}>
        {translate('resources.rip.review.discard')}
      </Button>
      <DiscardDialog
        open={open}
        message={
          fileCount > 0
            ? translate('resources.rip.review.confirmJob', {
                smart_count: fileCount,
              })
            : translate('resources.rip.review.confirmNoFiles')
        }
        busy={busy}
        onConfirm={handleConfirm}
        onCancel={() => setOpen(false)}
      />
    </>
  )
}
