import React, { useState } from 'react'
import {
  useDataProvider,
  useNotify,
  useRefresh,
  useTranslate,
} from 'react-admin'
import {
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  List,
  ListItem,
  ListItemText,
  TextField,
  Typography,
} from '@material-ui/core'
import { AlreadyHave } from './AlreadyHave'

// confirmItems are the items the dialog lists: every item of a big playlist, each with the
// Tracks the Library already seems to have for it, or else only the items with such Tracks.
const confirmItems = (preview) => {
  const have = preview?.alreadyHave || []
  if (!preview?.confirmCount) {
    return have
  }
  const byPosition = Object.fromEntries(have.map((it) => [it.position, it]))
  return (preview.titles || []).map((title, i) => ({
    position: i + 1,
    title,
    tracks: byPosition[i + 1]?.tracks,
  }))
}

// confirmMessage says why the dialog asks: a playlist of more than 50 items, Tracks the Library
// already seems to have, or both.
const confirmMessage = (translate, preview) => {
  const have = preview?.alreadyHave?.length || 0
  const messages = []
  if (preview?.confirmCount) {
    messages.push(
      translate('resources.rip.messages.confirm', {
        smart_count: preview.count,
      }),
    )
  }
  if (have > 0) {
    messages.push(
      preview.count > 1
        ? translate('resources.rip.messages.alreadyHaveSome', {
            smart_count: have,
          })
        : translate('resources.rip.messages.alreadyHaveThis'),
    )
  }
  return messages
}

// ConfirmDialog asks before ripping a playlist of more than 50 items, or what the Library already
// seems to have. A match is only a warning, so one click rips anyway.
const ConfirmDialog = ({ preview, sending, onConfirm, onCancel }) => {
  const translate = useTranslate()
  return (
    <Dialog open={!!preview} onClose={onCancel} scroll="paper">
      <DialogTitle>{preview?.title || preview?.url}</DialogTitle>
      <DialogContent dividers>
        {confirmMessage(translate, preview).map((message) => (
          <Typography key={message} paragraph>
            {message}
          </Typography>
        ))}
        <List dense>
          {confirmItems(preview).map((it) => (
            <ListItem key={it.position}>
              <ListItemText
                primary={it.title}
                secondary={<AlreadyHave tracks={it.tracks} />}
                secondaryTypographyProps={{ component: 'div' }}
              />
            </ListItem>
          ))}
        </List>
      </DialogContent>
      <DialogActions>
        <Button onClick={onCancel}>{translate('ra.action.cancel')}</Button>
        <Button
          onClick={onConfirm}
          color="primary"
          variant="contained"
          disabled={sending}
        >
          {preview?.confirmCount
            ? translate('resources.rip.actions.ripAll', {
                smart_count: preview?.count,
              })
            : translate('resources.rip.actions.ripAnyway')}
        </Button>
      </DialogActions>
    </Dialog>
  )
}

// RipForm queues a Rip of a video's, a playlist's or an album's URL. The job then runs in the
// background on the server. A playlist of more than 50 items, or what the Library already seems
// to have, is only queued once confirmed.
const RipForm = () => {
  const translate = useTranslate()
  const dataProvider = useDataProvider()
  const notify = useNotify()
  const refresh = useRefresh()
  const [url, setUrl] = useState('')
  const [sending, setSending] = useState(false)
  // preview is the server's listing of a playlist that needs confirmation.
  const [preview, setPreview] = useState(null)

  const send = (data) => {
    setSending(true)
    return dataProvider
      .create('rip', { data })
      .then(({ data: job }) => {
        setUrl('')
        setPreview(null)
        // A page that can't be read is saved as a failed job straight away.
        if (job?.status === 'failed') {
          notify(job.error || 'resources.rip.status.failed', 'warning')
        } else {
          notify('resources.rip.messages.queued', 'info')
        }
        refresh()
      })
      .catch((error) => {
        if (error?.status === 409 && error?.body?.needsConfirmation) {
          setPreview(error.body)
          return
        }
        setPreview(null)
        notify(
          error?.body?.error || error?.message || 'ra.page.error',
          'warning',
        )
      })
      .finally(() => setSending(false))
  }

  const handleSubmit = (e) => {
    e.preventDefault()
    const trimmed = url.trim()
    if (!trimmed) return
    send({ url: trimmed })
  }

  return (
    <Box component="form" onSubmit={handleSubmit} mt={2} mb={1}>
      <Box display="flex" alignItems="flex-start">
        <TextField
          id="rip-url"
          label={translate('resources.rip.fields.url')}
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="https://music.youtube.com/playlist?list=..."
          helperText={translate('resources.rip.messages.urlHelp')}
          variant="outlined"
          size="small"
          type="url"
          fullWidth
        />
        <Box ml={1}>
          <Button
            type="submit"
            variant="contained"
            color="primary"
            disabled={sending || !url.trim()}
          >
            {translate('resources.rip.actions.rip')}
          </Button>
        </Box>
      </Box>
      <Typography variant="caption" color="textSecondary">
        {translate('resources.rip.messages.responsibility')}
      </Typography>
      <ConfirmDialog
        preview={preview}
        sending={sending}
        onConfirm={() => send({ url: preview.url, confirm: true })}
        onCancel={() => setPreview(null)}
      />
    </Box>
  )
}

export default RipForm
