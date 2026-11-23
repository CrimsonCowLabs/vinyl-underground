import React, { useCallback, useEffect, useState } from 'react'
import PropTypes from 'prop-types'
import {
  Button,
  Confirm,
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
import BlockIcon from '@material-ui/icons/Block'
import BrokenImageIcon from '@material-ui/icons/BrokenImage'
import CloudUploadIcon from '@material-ui/icons/CloudUpload'
import ImageSearchIcon from '@material-ui/icons/ImageSearch'
import InsertPhotoIcon from '@material-ui/icons/InsertPhoto'
import subsonic from '../subsonic'
import { useLibraryEditing } from '../library/useLibraryEditing'

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
  placeholder: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
  },
  label: { marginTop: theme.spacing(0.5), fontWeight: 500 },
  note: { color: theme.palette.text.secondary },
  error: { color: theme.palette.error.main, marginRight: 'auto' },
  current: {
    display: 'flex',
    alignItems: 'center',
    gap: theme.spacing(2),
    marginTop: theme.spacing(2),
  },
  currentImage: {
    width: 120,
    height: 120,
    objectFit: 'cover',
    borderRadius: 6,
    background: theme.palette.action.hover,
  },
  currentActions: {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'flex-start',
  },
}))

// The sources, each asked on its own so a slow or failing one never holds the others up. The
// Cover Art Archive is only asked for albums with a MusicBrainz ID.
const sourcesFor = (record) =>
  record.mbzAlbumId || record.mbzReleaseGroupId
    ? ['local', 'external', 'coverartarchive']
    : ['local', 'external']

// A candidate's preview: { url } (a blob URL), { loading } or { failed }. It's fetched with the
// Authorization header rather than as an <img> URL, which would have to carry the login token.
// Each tile fetches its own, so a slow one never holds the others up. The blob URL is revoked,
// and a fetch under way cancelled, when the tile goes: when the picker closes or the candidates
// change.
const useCoverPreview = (albumId, candidateId) => {
  const dataProvider = useDataProvider()
  const [preview, setPreview] = useState({ loading: true })

  useEffect(() => {
    const controller = new AbortController()
    let url = null
    setPreview({ loading: true })
    dataProvider
      .getCoverPreview(albumId, { candidateId, signal: controller.signal })
      .then(({ data }) => {
        if (controller.signal.aborted) return
        url = URL.createObjectURL(data)
        setPreview({ url })
      })
      .catch(() => !controller.signal.aborted && setPreview({ failed: true }))
    return () => {
      controller.abort()
      if (url) URL.revokeObjectURL(url)
    }
  }, [dataProvider, albumId, candidateId])

  return preview
}

const knownErrors = [
  'unavailable',
  'invalid',
  'notFound',
  'coverChanged',
  'tooLarge',
]

// The refusals of a cover write (cover.jpg) or of Embed in Files (#164), worded in the picker.
const coverWriteErrors = [
  'editingOff',
  'invalidImage',
  'noAlbumFolder',
  'sharedFolder',
  'noFolderCover',
  'linked',
  'oldCopyLeft',
]

// The error message for a failed choice, upload, block or embed: a code the server answered
// with, with the action's own wording for some codes ({ notFound: 'albumNotFound' }).
const errorKey = (err, overrides = {}) => {
  const code = err?.body?.error
  if (coverWriteErrors.includes(code)) {
    return `resources.album.coverPicker.errors.${code}`
  }
  const key = overrides[code] ?? (knownErrors.includes(code) ? code : 'unknown')
  return `resources.correction.errors.${key}`
}

// What the album shows now, to mark wrong, the upload of an image of the admin's own, and Embed
// in Files. Writing (an upload, an embed) needs the album's library to be writable.
const CurrentCover = ({
  record,
  busy,
  writable,
  onMarkWrong,
  onUpload,
  onEmbed,
}) => {
  const classes = useStyles()
  const translate = useTranslate()
  return (
    <section className={classes.current}>
      <img
        className={classes.currentImage}
        src={subsonic.getCoverArtUrl(record, 300, true)}
        alt={translate('resources.album.coverPicker.current')}
      />
      <div className={classes.currentActions}>
        <Typography variant="subtitle1">
          {translate('resources.album.coverPicker.current')}
        </Typography>
        <MuiButton
          size="small"
          startIcon={<BlockIcon />}
          onClick={onMarkWrong}
          disabled={busy || !record.imageHash}
        >
          {translate('resources.album.coverPicker.markWrong')}
        </MuiButton>
        <MuiButton
          size="small"
          component="label"
          startIcon={<CloudUploadIcon />}
          disabled={busy || !writable}
        >
          {translate('resources.album.coverPicker.uploadButton')}
          <input
            type="file"
            accept="image/*"
            hidden
            disabled={busy || !writable}
            aria-label={translate('resources.album.coverPicker.upload')}
            onChange={(e) => {
              const file = e.target.files?.[0]
              e.target.value = ''
              if (file) onUpload(file)
            }}
          />
        </MuiButton>
        <MuiButton
          size="small"
          startIcon={<InsertPhotoIcon />}
          onClick={onEmbed}
          disabled={busy || !writable}
        >
          {translate('resources.album.coverPicker.embed')}
        </MuiButton>
      </div>
    </section>
  )
}

CurrentCover.propTypes = {
  record: PropTypes.object.isRequired,
  busy: PropTypes.bool,
  writable: PropTypes.bool,
  onMarkWrong: PropTypes.func.isRequired,
  onUpload: PropTypes.func.isRequired,
  onEmbed: PropTypes.func.isRequired,
}

// The note on a cover write that ended: written (with the server's warning, if another image
// still outranks it), unchanged, or still running in the background.
const writtenNote = (data, done) => {
  if (data?.background) {
    return ['resources.album.coverPicker.writingInBackground', { type: 'info' }]
  }
  if (data?.changed === false) {
    return ['resources.album.coverPicker.unchanged', { type: 'info' }]
  }
  if (data?.warning) {
    return [
      'resources.album.coverPicker.writtenWarning',
      { type: 'warning', messageArgs: { warning: data.warning } },
    ]
  }
  return [done, { type: 'info' }]
}

// The note on an Embed in Files job.
const embeddedNote = (data) => {
  if (data?.state !== 'done') {
    return [
      'resources.album.coverPicker.embeddingInBackground',
      { type: 'info' },
    ]
  }
  if (data.skipped > 0) {
    return [
      'resources.album.coverPicker.embeddedSkipped',
      { type: 'warning', messageArgs: { smart_count: data.skipped } },
    ]
  }
  return ['resources.album.coverPicker.embedded', { type: 'info' }]
}

const CandidateTile = ({ albumId, candidate, selected, onSelect }) => {
  const classes = useStyles()
  const translate = useTranslate()
  const sourceName = translate(
    `resources.album.coverPicker.candidateSources.${candidate.source}`,
    { _: candidate.source },
  )
  const preview = useCoverPreview(albumId, candidate.id)
  return (
    <ButtonBase
      className={`${classes.tile} ${selected ? classes.selected : ''}`}
      onClick={() => onSelect(candidate.id)}
      aria-pressed={selected}
    >
      {preview.url ? (
        <img
          className={classes.image}
          src={preview.url}
          alt={candidate.label}
        />
      ) : (
        <div
          className={`${classes.image} ${classes.placeholder}`}
          role="img"
          aria-label={candidate.label}
        >
          {preview.loading ? (
            <CircularProgress size={20} />
          ) : (
            <BrokenImageIcon color="disabled" />
          )}
        </div>
      )}
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
  const [confirmEmbed, setConfirmEmbed] = useState(false)
  // A cover is written to the album's folder (#164): only while its library is writable
  // (the writes wait for the server's answer; the note only once it is known).
  const { loaded, isWritable } = useLibraryEditing()
  const writable = isWritable(record.libraryId) === true
  const readOnly = loaded && !writable

  // Runs one of the dialog's actions, closing it once saved, or showing why it failed. note
  // is the notification for what the request answered.
  const save = useCallback(
    (request, note, errorFor) => {
      setSaving(true)
      setError(null)
      request()
        .then(({ data } = {}) => {
          onClose()
          notify(...note(data))
          refresh()
        })
        .catch((err) => {
          setError(translate(errorFor(err)))
          setSaving(false)
        })
    },
    [onClose, notify, refresh, translate],
  )

  const handleUse = useCallback(
    () =>
      save(
        () => dataProvider.chooseAlbumCover(record.id, selected),
        (data) => writtenNote(data, 'resources.album.coverPicker.chosen'),
        (err) =>
          errorKey(err, { notFound: 'coverNotFound', invalid: 'invalidImage' }),
      ),
    [save, dataProvider, record.id, selected],
  )
  const handleMarkWrong = useCallback(
    () =>
      save(
        () => dataProvider.blockAlbumCover(record.id, record.imageHash),
        () => ['resources.album.coverPicker.markedWrong', { type: 'info' }],
        (err) =>
          errorKey(err, {
            notFound: 'albumNotFound',
            invalid: 'unknown',
            alreadyCorrected: 'alreadyBlocked',
          }),
      ),
    [save, dataProvider, record.id, record.imageHash],
  )
  const handleUpload = useCallback(
    (file) =>
      save(
        () => dataProvider.uploadAlbumCover(record.id, file),
        (data) => writtenNote(data, 'resources.album.coverPicker.uploaded'),
        (err) =>
          errorKey(err, { notFound: 'albumNotFound', invalid: 'invalidImage' }),
      ),
    [save, dataProvider, record.id],
  )
  // Embed in Files rewrites every File of the album, so it asks first
  const handleEmbed = useCallback(() => {
    setConfirmEmbed(false)
    save(
      () => dataProvider.embedAlbumCover(record.id),
      embeddedNote,
      (err) => errorKey(err, { notFound: 'albumNotFound' }),
    )
  }, [save, dataProvider, record.id])

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
        {readOnly && (
          <DialogContentText className={classes.section}>
            {translate('resources.album.coverPicker.editingOff')}
          </DialogContentText>
        )}
        <CurrentCover
          record={record}
          busy={saving}
          writable={writable}
          onMarkWrong={handleMarkWrong}
          onUpload={handleUpload}
          onEmbed={() => setConfirmEmbed(true)}
        />
        <Confirm
          isOpen={confirmEmbed}
          title="resources.album.coverPicker.embedTitle"
          content="resources.album.coverPicker.embedContent"
          onConfirm={handleEmbed}
          onClose={() => setConfirmEmbed(false)}
        />
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
          disabled={saving || !selected || !writable}
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

// "Choose Cover" on an album page (admin only): lists the covers every source offers, and writes
// the chosen (or an uploaded) one to the album's folder as cover.jpg (#164), as an edit job.
// Embed in Files writes that cover into each of the album's Files; marking the cover wrong stays
// app data.
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
