import React from 'react'
import { useSelector } from 'react-redux'
import { useTranslate } from 'react-admin'
import { CircularProgress, Snackbar, SnackbarContent } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'

const useStyles = makeStyles((theme) => ({
  message: { display: 'flex', alignItems: 'center' },
  spinner: { marginRight: theme.spacing(2), flexShrink: 0 },
  dj: { fontWeight: theme.typography.fontWeightBold, display: 'block' },
}))

// StationPicking shows, with the player, that a Station is starting: with an
// LLM, the server waits up to a minute for the DJ's first picks. There is no
// "start anyway": past the minute the Station starts by itself.
const StationPicking = () => {
  const classes = useStyles()
  const translate = useTranslate()
  const picking = useSelector((state) => state.station?.picking)
  if (!picking) return null
  return (
    <Snackbar open anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}>
      <SnackbarContent
        role="status"
        message={
          <span className={classes.message}>
            <CircularProgress
              className={classes.spinner}
              size={24}
              color="inherit"
            />
            <span>
              <span className={classes.dj}>{picking.djName}</span>
              {translate('resources.station.picking')}
            </span>
          </span>
        }
      />
    </Snackbar>
  )
}

export default StationPicking
