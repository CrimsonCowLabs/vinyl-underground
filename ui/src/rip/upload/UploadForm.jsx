import React, { useRef, useState } from 'react'
import { useRefresh, useTranslate } from 'react-admin'
import {
  Box,
  Button,
  LinearProgress,
  List,
  ListItem,
  ListItemText,
  Typography,
} from '@material-ui/core'
import { UPLOAD_ACCEPT, uploadAll } from './uploadFiles'

// UploadForm lets the admin upload several Files at once into the Inbox (#35). They are sent
// one at a time, each with its own progress, and all land on one review card. Files of the wrong
// type or over 500 MB are refused before they are sent, and the server's reasons for any it
// refuses are shown beside them. upload is the function that sends them (for tests).
const UploadForm = ({ upload = uploadAll }) => {
  const translate = useTranslate()
  const refresh = useRefresh()
  const input = useRef(null)
  const [states, setStates] = useState([])
  const [busy, setBusy] = useState(false)

  const handleFiles = async (event) => {
    const files = Array.from(event.target.files || [])
    event.target.value = ''
    if (files.length === 0) return
    setBusy(true)
    try {
      const res = await upload(files, { onChange: setStates })
      if (res.some((s) => s.status === 'done')) refresh()
    } finally {
      setBusy(false)
    }
  }

  const statusText = (s) => {
    switch (s.status) {
      case 'failed':
        return translate(s.error, { _: s.error })
      case 'done':
        return translate('resources.rip.upload.done')
      case 'uploading':
        return translate('resources.rip.upload.progress', {
          percent: Math.round(s.progress * 100),
        })
      default:
        return translate('resources.rip.upload.waiting')
    }
  }

  return (
    <Box mb={2}>
      <Box display="flex" alignItems="center" style={{ gap: 8 }}>
        <input
          ref={input}
          type="file"
          multiple
          accept={UPLOAD_ACCEPT}
          hidden
          data-testid="upload-input"
          onChange={handleFiles}
        />
        <Button
          variant="outlined"
          color="primary"
          disabled={busy}
          onClick={() => input.current?.click()}
        >
          {translate(
            busy
              ? 'resources.rip.upload.uploading'
              : 'resources.rip.upload.button',
          )}
        </Button>
        <Typography variant="caption" color="textSecondary">
          {translate('resources.rip.upload.help')}
        </Typography>
      </Box>
      {states.length > 0 && (
        <List dense>
          {states.map((s, i) => (
            <ListItem key={`${i}-${s.name}`} data-testid={`upload-${i}`}>
              <ListItemText
                primary={s.name}
                secondary={
                  <>
                    <Typography
                      component="span"
                      variant="caption"
                      color={s.status === 'failed' ? 'error' : 'textSecondary'}
                    >
                      {statusText(s)}
                    </Typography>
                    {s.status === 'uploading' && (
                      <LinearProgress
                        variant="determinate"
                        value={Math.round(s.progress * 100)}
                      />
                    )}
                  </>
                }
              />
            </ListItem>
          ))}
        </List>
      )}
    </Box>
  )
}

export default UploadForm
