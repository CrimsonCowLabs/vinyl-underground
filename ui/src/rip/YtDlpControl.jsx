import React, { useEffect, useState } from 'react'
import { useNotify, useTranslate } from 'react-admin'
import { Box, Button, Typography } from '@material-ui/core'
import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'

const url = `${REST_URL}/rip/ytdlp`

// statusOf reads the server's {version, latest}. latest is null when the server couldn't look up
// the latest release.
const statusOf = (json) => ({
  version: json?.version ?? '',
  latest: json?.latest || null,
})

// YtDlpControl shows the installed yt-dlp's version, says whether a newer release is available,
// and updates it on demand. The server also updates it on a schedule, and after a failed Rip.
const YtDlpControl = () => {
  const translate = useTranslate()
  const notify = useNotify()
  const [status, setStatus] = useState(null)
  const [updating, setUpdating] = useState(false)
  const [loadError, setLoadError] = useState(null)

  useEffect(() => {
    let active = true
    httpClient(url)
      .then(({ json }) => active && setStatus(statusOf(json)))
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
        setStatus({ version: json.version, latest: null })
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
        // Read the new state back, so the notice compares the new version with the latest.
        return httpClient(url)
          .then(({ json }) => setStatus(statusOf(json)))
          .catch(() => {})
      })
      .catch((error) => {
        notify(
          error?.body?.error || error?.message || 'ra.page.error',
          'warning',
        )
      })
      .finally(() => setUpdating(false))
  }

  const version = status?.version
  const latest = status?.latest
  const newer = status != null && latest != null && latest !== version

  let label = ''
  if (loadError) {
    label = translate('resources.rip.ytdlp.unknown', { error: loadError })
  } else if (version === '') {
    label = newer
      ? translate('resources.rip.ytdlp.notInstalledAvailable', { latest })
      : translate('resources.rip.ytdlp.notInstalled')
  } else if (version && newer) {
    label = translate('resources.rip.ytdlp.versionAvailable', {
      version,
      latest,
    })
  } else if (version && latest) {
    label = translate('resources.rip.ytdlp.versionUpToDate', { version })
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
          variant={newer ? 'contained' : 'outlined'}
          color={newer ? 'primary' : 'default'}
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
