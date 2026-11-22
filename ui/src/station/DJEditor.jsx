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
  LinearProgress,
  TextField,
  Typography,
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
  genresHeader: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    flexWrap: 'wrap',
  },
  warning: { marginTop: theme.spacing(1) },
  setup: { padding: theme.spacing(2, 0) },
}))

// saveDJ creates the Listener's DJ, or edits it when it has an id. genres are
// only sent when the Listener picked them by hand; otherwise the server works
// them out from the name and description, or keeps the ones picked before,
// unless resuggest asks it to work them out again.
const saveDJ = (id, name, description, genres, resuggest) =>
  httpClient(id ? `${REST_URL}/station/dj/${id}` : `${REST_URL}/station/dj`, {
    method: id ? 'PUT' : 'POST',
    body: JSON.stringify({ name, description, genres, resuggest }),
  }).then(({ json }) => json)

// awaitSetup waits for the saved DJ's setup step on the server (with an LLM,
// up to a minute from the save) and gives how it ended: 'ready' when its
// Station starts with the LLM's picks, 'settingUp' when it plays a few Tracks
// by rule while the server keeps setting it up, 'none' when there was none.
const awaitSetup = (id) =>
  httpClient(`${REST_URL}/station/dj/${id}/setup`).then(
    ({ json }) => json.status,
  )

// openDJ fetches the Listener's DJ for the form. The server maps its genres
// first when they still need it (with the LLM, which can take a while).
const openDJ = (id) =>
  httpClient(`${REST_URL}/station/dj/${id}`).then(({ json }) => json)

const fetchLibraryGenres = () =>
  httpClient(`${REST_URL}/station/genres`).then(({ json }) => json)

// matchGenres asks the server which Library genres the name and description
// name, the ones saving would give the DJ.
const matchGenres = (name, description) =>
  httpClient(
    `${REST_URL}/station/genres/match?${new URLSearchParams({ name, description })}`,
  ).then(({ json }) => json)

// SetupStep shows how far the DJ's setup got: progress while the server maps
// its genres and picks its first hour, or that it is still being set up once
// that ran out and goes on in the background.
const SetupStep = ({ step }) => {
  const translate = useTranslate()
  const classes = useStyles()
  if (step === 'settingUp') {
    return (
      <Alert severity="info">
        {translate('resources.station.dj.setup.settingUp')}
      </Alert>
    )
  }
  return (
    <div className={classes.setup} role="status">
      <Typography variant="body1" gutterBottom>
        {translate(`resources.station.dj.setup.${step}`)}
      </Typography>
      <LinearProgress />
    </div>
  )
}

// DJEditor creates one of the Listener's own DJs (dj null) or edits one, from
// a name, a personality description and its genres. The genres follow the
// name and description until the Listener adds or removes one by hand; from
// then on they stay as picked, until the Listener asks for them to be
// suggested again from the description. onSaved gets the saved DJ.
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
  // matched is true once the genres shown are what matching the name and
  // description found, so an empty list means nothing matched.
  const [matched, setMatched] = useState(false)
  // suggesting is true while the server works the genres out.
  const [suggesting, setSuggesting] = useState(false)
  // resuggested is true once the Listener asked for the genres to be
  // suggested again, replacing the ones picked by hand, and didn't pick since.
  const [resuggested, setResuggested] = useState(false)
  const [library, setLibrary] = useState([])
  const [adding, setAdding] = useState('')
  const [saving, setSaving] = useState(false)
  // step is the setup step after saving, with an LLM: 'readingGenres' while
  // the save maps the genres, 'picking' while the server plans the first
  // hour, 'settingUp' once it ran out and goes on in the background; null
  // while editing. saved is the saved DJ meanwhile.
  const [step, setStep] = useState(null)
  const [saved, setSaved] = useState(null)
  // match counts genre matches, so only the latest one is shown, and none
  // once the Listener picks the genres by hand.
  const match = useRef(0)
  // editing counts the editor's sessions and saves, so a save or setup
  // answering after the editor was left or reopened is ignored.
  const editing = useRef(0)

  // showFound shows the genres the server found, unless the Listener picked
  // genres, or asked again, since.
  const showFound = (find) => {
    const id = ++match.current
    setSuggesting(true)
    find()
      .then((found) => {
        if (id !== match.current) return
        setGenres(found.genres || [])
        setPicked(!!found.genresEdited)
        setMatched(true)
        setSuggesting(false)
      })
      .catch(() => {
        if (id === match.current) setSuggesting(false)
      })
  }

  const runMatch = (forName, forDescription) =>
    showFound(() =>
      matchGenres(forName, forDescription).then((genres) => ({ genres })),
    )

  useEffect(() => {
    if (!open) return
    setName(dj?.name || '')
    setDescription(dj?.description || '')
    setGenres(dj?.genres || [])
    setPicked(!!dj?.genresEdited)
    setPickedNow(false)
    setMatched(false)
    setSuggesting(false)
    setResuggested(false)
    setAdding('')
    setStep(null)
    setSaved(null)
    setSaving(false)
    editing.current++
    match.current++
    // Opening a DJ maps genres it still needs (a DJ from before genres, or
    // one the LLM couldn't map yet), shown when the server answers.
    if (dj?.id) showFound(() => openDJ(dj.id))
    fetchLibraryGenres()
      .then(setLibrary)
      .catch(() => setLibrary([]))
  }, [open, dj])

  const valid = name.trim() !== '' && description.trim() !== ''

  const rematch = () => {
    if (!picked && valid) runMatch(name.trim(), description.trim())
  }

  const pick = (next) => {
    match.current++
    setGenres(next)
    setPicked(true)
    setPickedNow(true)
    setResuggested(false)
    setSuggesting(false)
  }

  // resuggest works the genres out again from the name and description,
  // replacing the ones picked by hand once saved.
  const resuggest = () => {
    setPicked(false)
    setPickedNow(false)
    setResuggested(true)
    runMatch(name.trim(), description.trim())
  }

  const save = (e) => {
    e.preventDefault()
    if (!valid) return
    const id = ++editing.current
    // With an LLM (which also enables Themes), the server sets the DJ up
    // before its Station plays: the save maps its genres, unless they were
    // picked by hand, then its first hour is picked.
    const withLLM = config.enableStationThemes
    setSaving(true)
    if (withLLM) setStep(picked ? 'picking' : 'readingGenres')
    saveDJ(
      dj?.id,
      name.trim(),
      description.trim(),
      pickedNow ? genres : undefined,
      resuggested || undefined,
    )
      .then((done) => {
        if (id !== editing.current) return
        setSaving(false)
        if (!withLLM) {
          onSaved(done)
          return
        }
        setSaved(done)
        setStep('picking')
        awaitSetup(done.id)
          .catch(() => 'settingUp')
          .then((status) => {
            if (id !== editing.current) return
            if (status === 'settingUp') setStep('settingUp')
            else onSaved(done)
          })
      })
      .catch((err) => {
        if (id !== editing.current) return
        setSaving(false)
        setStep(null)
        notify(stationErrorKey(err), { type: 'warning' })
      })
  }

  // close leaves the editor. Once the DJ is saved, its setup goes on without
  // the editor, so it counts as saved; while saving, it can't be left.
  const close = () => {
    if (saved) {
      editing.current++
      onSaved(saved)
    } else if (!saving) onClose()
  }

  const unmatched =
    matched && !picked && genres.length === 0 && !config.enableStationThemes

  return (
    <Dialog
      open={open}
      onClose={close}
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
          {step ? (
            <SetupStep step={step} />
          ) : (
            <>
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
                <div className={classes.genresHeader}>
                  <FormLabel component="legend">
                    {translate('resources.station.dj.fields.genres')}
                  </FormLabel>
                  <Button
                    size="small"
                    color="primary"
                    onClick={resuggest}
                    disabled={!valid || suggesting}
                  >
                    {translate('resources.station.dj.resuggest')}
                  </Button>
                </div>
                <div className={classes.tags}>
                  {genres.map((g) => (
                    <Chip
                      key={g}
                      data-testid="dj-genre"
                      size="small"
                      label={g}
                      onDelete={() =>
                        pick(genres.filter((other) => other !== g))
                      }
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
                  {translate(
                    suggesting
                      ? 'resources.station.dj.suggestingGenres'
                      : 'resources.station.dj.fields.genresTip',
                  )}
                </FormHelperText>
                {unmatched && (
                  <Alert severity="warning" className={classes.warning}>
                    {translate('resources.station.dj.noGenresWarning')}
                  </Alert>
                )}
              </div>
            </>
          )}
        </DialogContent>
        <DialogActions>
          {step === 'settingUp' && (
            <Button color="primary" onClick={close}>
              {translate('resources.station.dj.setup.done')}
            </Button>
          )}
          {!step && (
            <>
              <Button onClick={close}>{translate('ra.action.cancel')}</Button>
              <Button type="submit" color="primary" disabled={!valid || saving}>
                {translate('ra.action.save')}
              </Button>
            </>
          )}
        </DialogActions>
      </form>
    </Dialog>
  )
}

export default DJEditor
