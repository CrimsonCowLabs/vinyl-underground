import React, { useState } from 'react'
import {
  useDataProvider,
  useNotify,
  useRefresh,
  useTranslate,
} from 'react-admin'
import { Box, Button, TextField, Typography } from '@material-ui/core'

// RipForm queues a Rip of one video's URL. The job then runs in the background on the server.
const RipForm = () => {
  const translate = useTranslate()
  const dataProvider = useDataProvider()
  const notify = useNotify()
  const refresh = useRefresh()
  const [url, setUrl] = useState('')
  const [sending, setSending] = useState(false)

  const handleSubmit = (e) => {
    e.preventDefault()
    const trimmed = url.trim()
    if (!trimmed) return
    setSending(true)
    dataProvider
      .create('rip', { data: { url: trimmed } })
      .then(() => {
        setUrl('')
        notify('resources.rip.messages.queued', 'info')
        refresh()
      })
      .catch((error) => {
        notify(
          error?.body?.error || error?.message || 'ra.page.error',
          'warning',
        )
      })
      .finally(() => setSending(false))
  }

  return (
    <Box component="form" onSubmit={handleSubmit} mt={2} mb={1}>
      <Box display="flex" alignItems="flex-start">
        <TextField
          id="rip-url"
          label={translate('resources.rip.fields.url')}
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="https://music.youtube.com/watch?v=..."
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
    </Box>
  )
}

export default RipForm
