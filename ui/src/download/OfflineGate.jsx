import React from 'react'
import PropTypes from 'prop-types'
import { useLocation } from 'react-router-dom'
import { useTranslate } from 'react-admin'
import { Card, CardActions, CardContent, Typography } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import CloudOffIcon from '@material-ui/icons/CloudOff'
import { downloadsSupported, isAvailableOffline, useIsOffline } from './index'
import { ShuffleDownloadsButton } from './ShuffleDownloadsButton'

const useStyles = makeStyles((theme) => ({
  root: { marginTop: theme.spacing(2), maxWidth: 600 },
  title: { display: 'flex', alignItems: 'center', gap: theme.spacing(1) },
}))

// Says plainly that a page is unavailable offline, instead of showing it
// broken, and offers what does work
export const UnavailableOffline = () => {
  const translate = useTranslate()
  const classes = useStyles()
  return (
    <Card className={classes.root}>
      <CardContent>
        <Typography variant="h6" component="h2" className={classes.title}>
          <CloudOffIcon />
          {translate('download.offline.unavailableTitle')}
        </Typography>
        <Typography variant="body1">
          {translate('download.offline.unavailableContent')}
        </Typography>
      </CardContent>
      {downloadsSupported() && (
        <CardActions>
          <ShuffleDownloadsButton />
        </CardActions>
      )}
    </Card>
  )
}

// Shows the page, unless the device is offline and the page needs the server
export const OfflineGate = ({ children }) => {
  const offline = useIsOffline()
  const { pathname } = useLocation()
  return offline && !isAvailableOffline(pathname) ? (
    <UnavailableOffline />
  ) : (
    children
  )
}

OfflineGate.propTypes = {
  children: PropTypes.node,
}
