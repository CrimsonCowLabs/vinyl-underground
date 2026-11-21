import React, { useEffect, useState } from 'react'
import { useNotify, useTranslate } from 'react-admin'
import { Box, Button, Typography } from '@material-ui/core'
import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'

const url = `${REST_URL}/rip/ytdlp`

// YtDlpControl shows the installed yt-dlp's version and updates it on demand. The server also
// updates it on a schedule, and after a failed Rip.
const YtDlpControl = () => {
  const translate = useTranslate()
  const notify = useNotify()
  const [version, setVersion] = useState(null)
  const [updating, setUpdating] = useState(false)
  const [loadError, setLoadError] = useState(null)

  useEffect(() => {
    let active = true
    httpClient(url)
      .then(({ json }) => active && setVersion(json?.version ?? ''))
      .catch(
        (error) =>
          active &&
          setLoadError(error?.body?.error || error?.message || 'unknown'),
      )
    return () => {
      active = false
    }
  }, [])

  const handleUpdate = () => {
    setUpdating(true)
    httpClient(`${url}/update`, { method: 'POST' })
      .then(({ json }) => {
        setVersion(json.version)
        setLoadError(null)
        notify(
          translate(
            json.updated
              ? 'resources.rip.ytdlp.updated'
              : 'resources.rip.ytdlp.upToDate',
            { version: json.version },
          ),
          'info',
        )
      })
      .catch((error) => {
        notify(
          error?.body?.error || error?.message || 'ra.page.error',
          'warning',
        )
      })
      .finally(() => setUpdating(false))
  }

  let label = ''
  if (loadError) {
    label = translate('resources.rip.ytdlp.unknown', { error: loadError })
  } else if (version === '') {
    label = translate('resources.rip.ytdlp.notInstalled')
  } else if (version) {
    label = translate('resources.rip.ytdlp.version', { version })
  }

  return (
    <Box display="flex" alignItems="center" mt={2}>
      <Typography variant="body2" color="textSecondary">
        {label}
      </Typography>
      <Box ml={1}>
        <Button
          size="small"
          variant="outlined"
          onClick={handleUpdate}
          disabled={updating}
        >
          {translate(
            updating
              ? 'resources.rip.ytdlp.updating'
              : 'resources.rip.ytdlp.update',
          )}
        </Button>
      </Box>
    </Box>
  )
}

export default YtDlpControl
