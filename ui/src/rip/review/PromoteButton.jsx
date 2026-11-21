import React, { useState } from 'react'
import { useNotify, useRefresh, useTranslate } from 'react-admin'
import {
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  List,
  ListItem,
  ListItemText,
  Tooltip,
  Typography,
} from '@material-ui/core'
import config from '../../config'
import { httpClient } from '../../dataProvider'
import { errorText, jobUrl } from './reviewApi'

// PromoteButton is a ready job's "Promote all" (#36): once confirmed, it moves the job's Files from
// the Inbox into the Library's read-write folder, File by File. Files that fail stay in the Inbox,
// and a dialog says which and why; the card then shows only them (show), and the list refreshes,
// so a job with nothing left goes. It isn't there when Promote is off, and waits for unsaved edits
// (dirty) to be saved, as Promote moves the Files with the tags they have.
const PromoteButton = ({ jobId, review, dirty, show }) => {
  const translate = useTranslate()
  const notify = useNotify()
  const refresh = useRefresh()
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState(null)

  if (!config.enablePromote) return null

  const files = (review?.tracks || []).filter((t) => t.path)
  const byItem = Object.fromEntries(
    (review?.tracks || []).map((t) => [t.itemId, t]),
  )

  const handlePromote = () => {
    setBusy(true)
    httpClient(`${jobUrl(jobId)}/promote`, { method: 'POST' })
      .then(({ json }) => {
        setConfirming(false)
        if (json.promoted.length > 0) {
          notify('resources.rip.promote.done', 'info', {
            smart_count: json.promoted.length,
          })
        }
        if (json.failed.length > 0) setFailed(json.failed)
        if (json.review) show(json.review)
        refresh()
      })
      .catch((error) => notify(errorText(error), 'warning'))
      .finally(() => setBusy(false))
  }

  const button = (
    <Button
      color="primary"
      onClick={() => setConfirming(true)}
      disabled={dirty || busy || files.length === 0}
    >
      {translate(
        busy
          ? 'resources.rip.promote.promoting'
          : 'resources.rip.promote.button',
      )}
    </Button>
  )

  return (
    <>
      {dirty ? (
        <Tooltip title={translate('resources.rip.promote.unsaved')}>
          {/* A disabled button gets no hover events: the span does. */}
          <span>{button}</span>
        </Tooltip>
      ) : (
        button
      )}
      <Dialog open={confirming} onClose={() => setConfirming(false)}>
        <DialogTitle>{translate('resources.rip.promote.title')}</DialogTitle>
        <DialogContent>
          <Typography>
            {translate('resources.rip.promote.confirm', {
              smart_count: files.length,
            })}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setConfirming(false)}>
            {translate('ra.action.cancel')}
          </Button>
          <Button
            onClick={handlePromote}
            variant="contained"
            color="primary"
            disabled={busy}
          >
            {translate('resources.rip.promote.button')}
          </Button>
        </DialogActions>
      </Dialog>
      <Dialog
        open={!!failed}
        onClose={() => setFailed(null)}
        aria-labelledby={`promote-failed-${jobId}`}
      >
        <DialogTitle id={`promote-failed-${jobId}`}>
          {translate('resources.rip.promote.failedTitle')}
        </DialogTitle>
        <DialogContent>
          <Typography>
            {translate('resources.rip.promote.failedIntro', {
              smart_count: failed?.length || 0,
            })}
          </Typography>
          <List dense>
            {(failed || []).map((f) => {
              const t = byItem[f.itemId]
              const name = translate('resources.rip.promote.failedTrack', {
                position: t?.position ?? '?',
              })
              return (
                <ListItem key={f.itemId}>
                  <ListItemText
                    primary={t?.title ? `${name}: ${t.title}` : name}
                    secondary={f.error}
                  />
                </ListItem>
              )
            })}
          </List>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setFailed(null)}>
            {translate('ra.action.close')}
          </Button>
        </DialogActions>
      </Dialog>
    </>
  )
}

export default PromoteButton
