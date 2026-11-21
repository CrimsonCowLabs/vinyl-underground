import React from 'react'
import { useTranslate } from 'react-admin'
import { Chip } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import CloudOffIcon from '@material-ui/icons/CloudOff'
import { downloadsSupported, useIsOffline } from './index'
import { ShuffleDownloadsButton } from './ShuffleDownloadsButton'

const useStyles = makeStyles((theme) => ({
  chip: {
    color: 'inherit',
    borderColor: 'currentColor',
    marginLeft: theme.spacing(1),
  },
  icon: { color: 'inherit' },
}))

// Shown in the app bar while the device is offline, with the one thing that
// always works offline: shuffling the Downloads
export const OfflineIndicator = () => {
  const translate = useTranslate()
  const classes = useStyles()
  const offline = useIsOffline()

  if (!offline) return null

  return (
    <>
      <Chip
        className={classes.chip}
        classes={{ icon: classes.icon }}
        variant="outlined"
        size="small"
        icon={<CloudOffIcon />}
        label={translate('download.offline.indicator')}
      />
      {downloadsSupported() && <ShuffleDownloadsButton compact />}
    </>
  )
}
