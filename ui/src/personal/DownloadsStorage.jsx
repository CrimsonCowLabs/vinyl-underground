import React from 'react'
import { useTranslate } from 'react-admin'
import { Link } from 'react-router-dom'
import {
  FormControl,
  FormHelperText,
  FormLabel,
  LinearProgress,
  List,
  ListItem,
  ListItemText,
  Typography,
} from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import { downloadsSupported, useDownloadUsage } from '../download'
import { formatBytes } from '../utils'

const useStyles = makeStyles((theme) => ({
  root: { width: '100%', marginBottom: theme.spacing(2) },
  meter: { margin: theme.spacing(1, 0) },
  list: { maxWidth: 600 },
}))

// The storage meter: how much space Downloads use on this device, in total
// and for each Playlist or album. There is no cap (ADR-0006), so this only
// informs.
export const DownloadsStorage = () => {
  const translate = useTranslate()
  const classes = useStyles()
  const usage = useDownloadUsage()

  if (!downloadsSupported()) return null

  const device = usage?.device
  const byLargest = [...(usage?.downloads || [])].sort(
    (a, b) => b.size - a.size,
  )

  return (
    <FormControl className={classes.root}>
      <FormLabel>{translate('download.storage.title')}</FormLabel>
      {!usage ? (
        <Typography variant="body2">
          {translate('download.storage.measuring')}
        </Typography>
      ) : (
        <>
          <Typography variant="body1">
            {translate('download.storage.total', {
              size: formatBytes(usage.total),
            })}
          </Typography>
          {device && (
            <>
              <LinearProgress
                className={classes.meter}
                variant="determinate"
                value={Math.min(100, (100 * device.used) / device.quota)}
              />
              <Typography variant="body2" color="textSecondary">
                {translate('download.storage.device', {
                  used: formatBytes(device.used),
                  quota: formatBytes(device.quota),
                })}
              </Typography>
            </>
          )}
          {byLargest.length === 0 ? (
            <Typography variant="body2">
              {translate('download.storage.none')}
            </Typography>
          ) : (
            <List dense className={classes.list}>
              {byLargest.map((d) => (
                <ListItem
                  key={`${d.kind}:${d.id}`}
                  button
                  component={Link}
                  to={`/${d.kind}/${d.id}/show`}
                >
                  <ListItemText
                    primary={d.name}
                    secondary={translate(`download.kind.${d.kind}`)}
                  />
                  <Typography variant="body2">{formatBytes(d.size)}</Typography>
                </ListItem>
              ))}
            </List>
          )}
          <FormHelperText>
            {translate('download.storage.noCap')}{' '}
            {byLargest.length > 1 && translate('download.storage.shared')}
          </FormHelperText>
        </>
      )}
    </FormControl>
  )
}
