import React, { useEffect, useState } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { Title, useNotify, useTranslate } from 'react-admin'
import {
  Button,
  Card,
  CardActions,
  CardContent,
  Chip,
  Grid,
  TextField,
  Typography,
} from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import PlayArrowIcon from '@material-ui/icons/PlayArrow'
import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'
import config from '../config'
import { setPlayMode } from '../actions'
import { startStation } from './stationState'
import { fetchStationTracks } from './useStationRefill'

// How many Tracks a Station starts with: the one to play and a few queued.
export const START_COUNT = 5
// The longest Theme the server accepts.
export const MAX_THEME_LENGTH = 200

const useStyles = makeStyles((theme) => ({
  root: { padding: theme.spacing(2) },
  card: { height: '100%', display: 'flex', flexDirection: 'column' },
  content: { flexGrow: 1 },
  onAir: { marginLeft: theme.spacing(1) },
  theme: { marginBottom: theme.spacing(2), maxWidth: 480 },
}))

const errorKey = (err) => {
  const code = err?.body?.error
  return ['noTracks', 'unknownDJ', 'invalidTheme'].includes(code)
    ? `resources.station.errors.${code}`
    : 'resources.station.errors.unavailable'
}

// StationList shows the DJs. Starting one replaces the play queue with its
// Station, which then keeps refilling from the server (see useStationRefill).
// With an LLM configured, the Listener can also give a Theme for the Station.
const StationList = () => {
  const translate = useTranslate()
  const notify = useNotify()
  const dispatch = useDispatch()
  const classes = useStyles()
  const onAir = useSelector((state) => state.station?.djId)
  const [djs, setDJs] = useState([])
  const [starting, setStarting] = useState(null)
  const [stationTheme, setStationTheme] = useState('')

  useEffect(() => {
    httpClient(`${REST_URL}/station/dj`)
      .then(({ json }) => setDJs(json))
      .catch(() =>
        notify('resources.station.errors.unavailable', { type: 'warning' }),
      )
  }, [notify])

  const start = (dj) => {
    const theme = config.enableStationThemes ? stationTheme.trim() : ''
    setStarting(dj.id)
    fetchStationTracks(dj.id, START_COUNT, theme)
      .then((tracks) => {
        // Shuffle or repeat would replay Tracks and break the DJ's order.
        dispatch(setPlayMode('order'))
        dispatch(startStation(dj.id, tracks, theme))
      })
      .catch((err) => notify(errorKey(err), { type: 'warning' }))
      .finally(() => setStarting(null))
  }

  return (
    <div className={classes.root}>
      <Title title={translate('resources.station.name', { smart_count: 2 })} />
      {config.enableStationThemes && (
        <TextField
          id="station-theme"
          className={classes.theme}
          label={translate('resources.station.fields.theme')}
          helperText={translate('resources.station.fields.themeHelp')}
          value={stationTheme}
          onChange={(e) => setStationTheme(e.target.value)}
          inputProps={{ maxLength: MAX_THEME_LENGTH }}
          fullWidth
        />
      )}
      <Grid container spacing={2}>
        {djs.map((dj) => (
          <Grid item xs={12} sm={6} md={4} key={dj.id}>
            <Card className={classes.card}>
              <CardContent className={classes.content}>
                <Typography variant="h6" component="h2">
                  {dj.name}
                  {onAir === dj.id && (
                    <Chip
                      className={classes.onAir}
                      size="small"
                      color="primary"
                      label={translate('resources.station.onAir')}
                    />
                  )}
                </Typography>
                <Typography variant="body2" color="textSecondary">
                  {dj.description}
                </Typography>
              </CardContent>
              <CardActions>
                <Button
                  color="primary"
                  startIcon={<PlayArrowIcon />}
                  disabled={starting !== null}
                  aria-label={`${translate('resources.station.actions.start')} ${dj.name}`}
                  onClick={() => start(dj)}
                >
                  {translate('resources.station.actions.start')}
                </Button>
              </CardActions>
            </Card>
          </Grid>
        ))}
      </Grid>
    </div>
  )
}

export default StationList
