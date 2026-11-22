import React, { useEffect, useRef, useState } from 'react'
import { useNotify, useTranslate } from 'react-admin'
import {
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormHelperText,
  FormLabel,
  TextField,
} from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import Alert from '@material-ui/lab/Alert'
import Autocomplete from '@material-ui/lab/Autocomplete'
import CancelIcon from '@material-ui/icons/Cancel'
import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'
import config from '../config'
import { stationErrorKey } from './stationErrors'

// The longest DJ name and personality description the server accepts.
export const MAX_DJ_NAME_LENGTH = 60
export const MAX_DJ_DESCRIPTION_LENGTH = 1000

const useStyles = makeStyles((theme) => ({
  genres: { marginTop: theme.spacing(2) },
  tags: {
    display: 'flex',
    flexWrap: 'wrap',
    gap: theme.spacing(0.5),
    margin: theme.spacing(1, 0),
  },
  warning: { marginTop: theme.spacing(1) },
}))

// saveDJ creates the Listener's DJ, or edits it when it has an id. genres are
// only sent when the Listener picked them by hand; otherwise the server works
// them out from the name and description, or keeps the ones picked before.
const saveDJ = (id, name, description, genres) =>
  httpClient(id ? `${REST_URL}/station/dj/${id}` : `${REST_URL}/station/dj`, {
    method: id ? 'PUT' : 'POST',
    body: JSON.stringify({ name, description, genres }),
  }).then(({ json }) => json)

const fetchLibraryGenres = () =>
  httpClient(`${REST_URL}/station/genres`).then(({ json }) => json)

// matchGenres asks the server which Library genres the name and description
// name, the ones saving would give the DJ.
const matchGenres = (name, description) =>
  httpClient(
    `${REST_URL}/station/genres/match?${new URLSearchParams({ name, description })}`,
  ).then(({ json }) => json)

// DJEditor creates one of the Listener's own DJs (dj null) or edits one, from
// a name, a personality description and its genres. The genres follow the
// name and description until the Listener adds or removes one by hand; from
// then on they stay as picked. onSaved gets the saved DJ.
const DJEditor = ({ open, dj, onClose, onSaved }) => {
  const translate = useTranslate()
  const notify = useNotify()
  const classes = useStyles()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [genres, setGenres] = useState([])
  // picked is true once the genres were picked by hand, now or before.
  const [picked, setPicked] = useState(false)
  // pickedNow is true once the Listener picked them in this editor, so they
  // are sent with the DJ.
  const [pickedNow, setPickedNow] = useState(false)
  const [library, setLibrary] = useState([])
  const [adding, setAdding] = useState('')
  const [saving, setSaving] = useState(false)
  // match counts genre matches, so only the latest one is shown, and none
  // once the Listener picks the genres by hand.
  const match = useRef(0)

  useEffect(() => {
    if (!open) return
    setName(dj?.name || '')
    setDescription(dj?.description || '')
    setGenres(dj?.genres || [])
    setPicked(!!dj?.genresEdited)
    setPickedNow(false)
    setAdding('')
    match.current++
    fetchLibraryGenres()
      .then(setLibrary)
      .catch(() => setLibrary([]))
  }, [open, dj])

  const valid = name.trim() !== '' && description.trim() !== ''

  const rematch = () => {
    if (picked || !valid) return
    const id = ++match.current
    matchGenres(name.trim(), description.trim())
      .then((matched) => {
        if (id === match.current) setGenres(matched)
      })
      .catch(() => {})
  }

  const pick = (next) => {
    match.current++
    setGenres(next)
    setPicked(true)
    setPickedNow(true)
  }

  const save = (e) => {
    e.preventDefault()
    if (!valid) return
    setSaving(true)
    saveDJ(
      dj?.id,
      name.trim(),
      description.trim(),
      pickedNow ? genres : undefined,
    )
      .then((saved) => {
        setSaving(false)
        onSaved(saved)
      })
      .catch((err) => {
        setSaving(false)
        notify(stationErrorKey(err), { type: 'warning' })
      })
  }

  const unmatched =
    genres.length === 0 &&
    description.trim() !== '' &&
    !config.enableStationThemes

  return (
    <Dialog
      open={open}
      onClose={onClose}
      aria-labelledby="dj-editor-title"
      fullWidth
      maxWidth="sm"
    >
      <form onSubmit={save}>
        <DialogTitle id="dj-editor-title">
          {translate(
            dj ? 'resources.station.dj.edit' : 'resources.station.dj.new',
          )}
        </DialogTitle>
        <DialogContent>
          <TextField
            id="dj-name"
            label={translate('resources.station.dj.fields.name')}
            value={name}
            onChange={(e) => setName(e.target.value)}
            onBlur={rematch}
            inputProps={{ maxLength: MAX_DJ_NAME_LENGTH }}
            margin="normal"
            autoFocus
            required
            fullWidth
          />
          <TextField
            id="dj-description"
            label={translate('resources.station.dj.fields.description')}
            helperText={translate(
              'resources.station.dj.fields.descriptionHelp',
            )}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            onBlur={rematch}
            inputProps={{ maxLength: MAX_DJ_DESCRIPTION_LENGTH }}
            margin="normal"
            minRows={3}
            multiline
            required
            fullWidth
          />
          <div className={classes.genres}>
            <FormLabel component="legend">
              {translate('resources.station.dj.fields.genres')}
            </FormLabel>
            <div className={classes.tags}>
              {genres.map((g) => (
                <Chip
                  key={g}
                  data-testid="dj-genre"
                  size="small"
                  label={g}
                  onDelete={() => pick(genres.filter((other) => other !== g))}
                  deleteIcon={
                    <CancelIcon
                      role="button"
                      aria-hidden={false}
                      aria-label={`${translate('ra.action.remove')} ${g}`}
                    />
                  }
                />
              ))}
            </div>
            <Autocomplete
              id="dj-add-genre"
              options={library.filter((g) => !genres.includes(g))}
              value={null}
              inputValue={adding}
              onInputChange={(e, value) => setAdding(value)}
              onChange={(e, g) => {
                if (g) pick([...genres, g])
                setAdding('')
              }}
              renderInput={(params) => (
                <TextField
                  {...params}
                  label={translate('resources.station.dj.fields.addGenre')}
                  size="small"
                />
              )}
            />
            <FormHelperText>
              {translate('resources.station.dj.fields.genresTip')}
            </FormHelperText>
            {unmatched && (
              <Alert severity="warning" className={classes.warning}>
                {translate('resources.station.dj.noGenresWarning')}
              </Alert>
            )}
          </div>
        </DialogContent>
        <DialogActions>
          <Button onClick={onClose}>{translate('ra.action.cancel')}</Button>
          <Button type="submit" color="primary" disabled={!valid || saving}>
            {translate('ra.action.save')}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  )
}

export default DJEditor
