import React, { useCallback, useEffect, useState } from 'react'
import PropTypes from 'prop-types'
import {
  Button,
  useDataProvider,
  useNotify,
  usePermissions,
  useRefresh,
  useTranslate,
} from 'react-admin'
import {
  Button as MuiButton,
  ButtonBase,
  Chip,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Typography,
} from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import ImageSearchIcon from '@material-ui/icons/ImageSearch'
import { REST_URL } from '../consts'
import { baseUrl } from '../utils'

const useStyles = makeStyles((theme) => ({
  section: { marginTop: theme.spacing(2) },
  grid: {
    display: 'grid',
    gridTemplateColumns: 'repeat(auto-fill, minmax(140px, 1fr))',
    gap: theme.spacing(1.5),
    marginTop: theme.spacing(1),
  },
  tile: {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'stretch',
    textAlign: 'left',
    padding: theme.spacing(0.5),
    borderRadius: 6,
    border: '2px solid transparent',
  },
  selected: { borderColor: theme.palette.primary.main },
  image: {
    width: '100%',
    aspectRatio: '1 / 1',
    objectFit: 'cover',
    borderRadius: 6,
    background: theme.palette.action.hover,
  },
  label: { marginTop: theme.spacing(0.5), fontWeight: 500 },
  note: { color: theme.palette.text.secondary },
  error: { color: theme.palette.error.main, marginRight: 'auto' },
}))

// The sources, each asked on its own so a slow or failing one never holds the others up. The
// Cover Art Archive is only asked for albums with a MusicBrainz ID.
const sourcesFor = (record) =>
  record.mbzAlbumId || record.mbzReleaseGroupId
    ? ['local', 'external', 'coverartarchive']
    : ['local', 'external']

// An <img> can't send the Authorization header, so the preview carries the token, as the event
// stream does.
const coverPreviewUrl = (albumId, candidateId) => {
  const url = baseUrl(
    `${REST_URL}/albumCover/${albumId}/candidates/${candidateId}/preview`,
  )
  const token = localStorage.getItem('token')
  return token ? `${url}?jwt=${token}` : url
}

const knownErrors = ['unavailable', 'invalid', 'notFound']

const CandidateTile = ({ albumId, candidate, selected, onSelect }) => {
  const classes = useStyles()
  const translate = useTranslate()
  const sourceName = translate(
    `resources.album.coverPicker.candidateSources.${candidate.source}`,
    { _: candidate.source },
  )
  return (
    <ButtonBase
      className={`${classes.tile} ${selected ? classes.selected : ''}`}
      onClick={() => onSelect(candidate.id)}
      aria-pressed={selected}
    >
      <img
        className={classes.image}
        src={coverPreviewUrl(albumId, candidate.id)}
        alt={candidate.label}
        loading="lazy"
      />
      <Typography variant="body2" className={classes.label}>
        {candidate.label}
      </Typography>
      {candidate.detail && (
        <Typography variant="caption" className={classes.note}>
          {candidate.detail}
        </Typography>
      )}
      {sourceName !== candidate.label && (
        <Typography variant="caption" className={classes.note}>
          {sourceName}
        </Typography>
      )}
      {candidate.exactRelease && (
        <Chip
          size="small"
          color="primary"
          label={translate('resources.album.coverPicker.exactRelease')}
        />
      )}
    </ButtonBase>
  )
}

CandidateTile.propTypes = {
  albumId: PropTypes.string.isRequired,
  candidate: PropTypes.object.isRequired,
  selected: PropTypes.bool,
  onSelect: PropTypes.func.isRequired,
}

// One source's candidates: a spinner until it answers, then its covers, or why there are none.
const CandidateSection = ({ albumId, source, selected, onSelect }) => {
  const classes = useStyles()
  const translate = useTranslate()
  const dataProvider = useDataProvider()
  const [state, setState] = useState({ loading: true })

  useEffect(() => {
    let live = true
    dataProvider
      .getCoverCandidates(albumId, source)
      .then(({ data }) => live && setState({ loading: false, ...data }))
      .catch(() => live && setState({ loading: false, failed: true }))
    return () => {
      live = false
    }
  }, [dataProvider, albumId, source])

  const candidates = state.candidates || []
  let note = null
  if (state.loading) {
    note = <CircularProgress size={20} />
  } else if (state.failed) {
    note = translate(
      candidates.length
        ? 'resources.album.coverPicker.partial'
        : 'resources.album.coverPicker.failed',
    )
  } else if (!candidates.length) {
    note = translate('resources.album.coverPicker.empty')
  }

  return (
    <section className={classes.section}>
      <Typography variant="subtitle1">
        {translate(`resources.album.coverPicker.sources.${source}`)}
      </Typography>
      {note && (
        <Typography variant="body2" className={classes.note} component="div">
          {note}
        </Typography>
      )}
      {candidates.length > 0 && (
        <div className={classes.grid}>
          {candidates.map((c) => (
            <CandidateTile
              key={c.id}
              albumId={albumId}
              candidate={c}
              selected={selected === c.id}
              onSelect={onSelect}
            />
          ))}
        </div>
      )}
    </section>
  )
}

CandidateSection.propTypes = {
  albumId: PropTypes.string.isRequired,
  source: PropTypes.string.isRequired,
  selected: PropTypes.string,
  onSelect: PropTypes.func.isRequired,
}

const ChooseCoverDialog = ({ record, onClose }) => {
  const translate = useTranslate()
  const dataProvider = useDataProvider()
  const notify = useNotify()
  const refresh = useRefresh()
  const [selected, setSelected] = useState(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState(null)

  const handleUse = useCallback(() => {
    setSaving(true)
    setError(null)
    dataProvider
      .chooseAlbumCover(record.id, selected)
      .then(() => {
        onClose()
        notify('resources.album.coverPicker.chosen', { type: 'info' })
        refresh()
      })
      .catch((err) => {
        const code = err?.body?.error
        setError(
          translate(
            code === 'notFound'
              ? 'resources.correction.errors.coverNotFound'
              : `resources.correction.errors.${knownErrors.includes(code) ? code : 'unknown'}`,
          ),
        )
        setSaving(false)
      })
  }, [dataProvider, record.id, selected, onClose, notify, refresh, translate])

  const classes = useStyles()
  return (
    <Dialog
      open
      onClose={onClose}
      fullWidth
      maxWidth="md"
      aria-labelledby="choose-cover-title"
    >
      <DialogTitle id="choose-cover-title">
        {translate('resources.album.coverPicker.title', { name: record.name })}
      </DialogTitle>
      <DialogContent>
        <DialogContentText>
          {translate('resources.album.coverPicker.help')}
        </DialogContentText>
        {sourcesFor(record).map((source) => (
          <CandidateSection
            key={source}
            albumId={record.id}
            source={source}
            selected={selected}
            onSelect={setSelected}
          />
        ))}
      </DialogContent>
      <DialogActions>
        {error && (
          <Typography variant="body2" className={classes.error}>
            {error}
          </Typography>
        )}
        <MuiButton onClick={onClose} disabled={saving}>
          {translate('ra.action.cancel')}
        </MuiButton>
        <MuiButton
          color="primary"
          variant="contained"
          onClick={handleUse}
          disabled={saving || !selected}
          startIcon={saving ? <CircularProgress size={16} /> : null}
        >
          {translate('resources.album.coverPicker.use')}
        </MuiButton>
      </DialogActions>
    </Dialog>
  )
}

ChooseCoverDialog.propTypes = {
  record: PropTypes.object.isRequired,
  onClose: PropTypes.func.isRequired,
}

// "Choose Cover" on an album page (admin only): lists the covers every source offers, and saves
// the chosen one as an album cover Correction, which wins over the usual cover order.
export const ChooseCoverButton = ({ record, className, size }) => {
  const translate = useTranslate()
  const { permissions } = usePermissions()
  const [open, setOpen] = useState(false)
  const handleClose = useCallback(() => setOpen(false), [])

  if (permissions !== 'admin' || !record?.id) return null

  return (
    <>
      <Button
        className={className}
        size={size}
        onClick={() => setOpen(true)}
        label={translate('resources.album.actions.chooseCover')}
        disabled={record.missing}
      >
        <ImageSearchIcon />
      </Button>
      {open && <ChooseCoverDialog record={record} onClose={handleClose} />}
    </>
  )
}

ChooseCoverButton.propTypes = {
  record: PropTypes.object,
  className: PropTypes.string,
  size: PropTypes.oneOf(['small', 'medium']),
}

export default ChooseCoverButton
