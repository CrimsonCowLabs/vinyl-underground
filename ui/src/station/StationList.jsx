import React, { useCallback, useEffect, useState } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { Title, useNotify, useTranslate } from 'react-admin'
import {
  Button,
  Card,
  CardActions,
  CardContent,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Grid,
  IconButton,
  TextField,
  Typography,
} from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import AddIcon from '@material-ui/icons/Add'
import DeleteIcon from '@material-ui/icons/Delete'
import EditIcon from '@material-ui/icons/Edit'
import PlayArrowIcon from '@material-ui/icons/PlayArrow'
import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'
import config from '../config'
import { setPlayMode } from '../actions'
import { startStation, stopStation } from './stationState'
import { fetchStationTracks } from './useStationRefill'
import DJEditor from './DJEditor'

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
  toolbar: { marginBottom: theme.spacing(2) },
  spacer: { flexGrow: 1 },
}))

const errorKey = (err) => {
  const code = err?.body?.error
  return ['noTracks', 'unknownDJ', 'invalidTheme'].includes(code)
    ? `resources.station.errors.${code}`
    : 'resources.station.errors.unavailable'
}

// StationList shows the DJs: the built-in ones, then the Listener's own, which
// they can create, edit and delete here (built-in DJs are read-only). Starting
// one replaces the play queue with its Station, which then keeps refilling from
// the server (see useStationRefill). With an LLM configured, the Listener can
// also give a Theme for the Station.
const StationList = () => {
  const translate = useTranslate()
  const notify = useNotify()
  const dispatch = useDispatch()
  const classes = useStyles()
  const onAir = useSelector((state) => state.station?.djId)
  const [djs, setDJs] = useState([])
  const [starting, setStarting] = useState(null)
  const [stationTheme, setStationTheme] = useState('')
  // editing is { dj } while the editor is open; dj is null for a new one.
  const [editing, setEditing] = useState(null)
  const [deleting, setDeleting] = useState(null)

  const loadDJs = useCallback(
    () =>
      httpClient(`${REST_URL}/station/dj`)
        .then(({ json }) => setDJs(json))
        .catch(() =>
          notify('resources.station.errors.unavailable', { type: 'warning' }),
        ),
    [notify],
  )

  useEffect(() => {
    loadDJs()
  }, [loadDJs])

  const saved = () => {
    setEditing(null)
    loadDJs()
  }

  const remove = () => {
    const dj = deleting
    setDeleting(null)
    httpClient(`${REST_URL}/station/dj/${dj.id}`, { method: 'DELETE' })
      .then(() => {
        // Its Station can't refill any more; what's queued plays out.
        if (onAir === dj.id) dispatch(stopStation())
        loadDJs()
      })
      .catch(() =>
        notify('resources.station.errors.unavailable', { type: 'warning' }),
      )
  }

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
      <Grid container className={classes.toolbar}>
        <div className={classes.spacer} />
        <Button
          color="primary"
          variant="outlined"
          startIcon={<AddIcon />}
          onClick={() => setEditing({ dj: null })}
        >
          {translate('resources.station.dj.new')}
        </Button>
      </Grid>
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
                {!dj.builtIn && (
                  <>
                    <div className={classes.spacer} />
                    <IconButton
                      size="small"
                      aria-label={`${translate('ra.action.edit')} ${dj.name}`}
                      onClick={() => setEditing({ dj })}
                    >
                      <EditIcon fontSize="small" />
                    </IconButton>
                    <IconButton
                      size="small"
                      aria-label={`${translate('ra.action.delete')} ${dj.name}`}
                      onClick={() => setDeleting(dj)}
                    >
                      <DeleteIcon fontSize="small" />
                    </IconButton>
                  </>
                )}
              </CardActions>
            </Card>
          </Grid>
        ))}
      </Grid>
      <DJEditor
        open={editing !== null}
        dj={editing?.dj}
        onClose={() => setEditing(null)}
        onSaved={saved}
      />
      <Dialog
        open={deleting !== null}
        onClose={() => setDeleting(null)}
        aria-labelledby="dj-delete-title"
      >
        <DialogTitle id="dj-delete-title">
          {translate('resources.station.dj.deleteTitle', {
            name: deleting?.name,
          })}
        </DialogTitle>
        <DialogContent>
          <DialogContentText>
            {translate('resources.station.dj.deleteContent')}
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleting(null)}>
            {translate('ra.action.cancel')}
          </Button>
          <Button color="primary" onClick={remove}>
            {translate('ra.action.confirm')}
          </Button>
        </DialogActions>
      </Dialog>
    </div>
  )
}

export default StationList
