import React, { useCallback, useEffect, useState } from 'react'
import { useNotify, useRefresh, useTranslate } from 'react-admin'
import {
  Box,
  Button,
  Card,
  CardActions,
  CardContent,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TextField,
  Tooltip,
  Typography,
} from '@material-ui/core'
import DeleteIcon from '@material-ui/icons/Delete'
import { httpClient } from '../../dataProvider'
import { REST_URL } from '../../consts'
import {
  editsFrom,
  initialValues,
  invalidFields,
  setAlbumField,
  setTrackField,
} from './reviewForm'

const jobUrl = (jobId) => `${REST_URL}/rip/${jobId}`

const errorText = (error) =>
  error?.body?.error || error?.message || 'ra.page.error'

// DiscardDialog asks before deleting Files for good: they don't go to Quarantine.
export const DiscardDialog = ({ open, message, busy, onConfirm, onCancel }) => {
  const translate = useTranslate()
  return (
    <Dialog open={open} onClose={onCancel}>
      <DialogTitle>
        {translate('resources.rip.review.discardTitle')}
      </DialogTitle>
      <DialogContent>
        <Typography>{message}</Typography>
      </DialogContent>
      <DialogActions>
        <Button onClick={onCancel}>{translate('ra.action.cancel')}</Button>
        <Button
          onClick={onConfirm}
          variant="contained"
          color="secondary"
          disabled={busy}
        >
          {translate('resources.rip.review.discard')}
        </Button>
      </DialogActions>
    </Dialog>
  )
}

// ReviewCard is a finished job's review card: its album-level tags and its Track list, read from
// the Files, to edit and save into them, and Discard for the whole job or one Track. Only the
// admin sees Rips at all.
//
// Slots for later work: trackExtra(track) renders next to a Track's title (e.g. #32's "already
// have" indicator), and actions are more buttons beside Save and Discard (e.g. #36's "Promote
// all"). Proposed tags (#34) go into the same form state (see reviewForm.js).
const ReviewCard = ({ record, trackExtra, actions }) => {
  const translate = useTranslate()
  const notify = useNotify()
  const refresh = useRefresh()
  const [review, setReview] = useState(null)
  const [values, setValues] = useState(null)
  const [loadError, setLoadError] = useState(null)
  const [saving, setSaving] = useState(false)
  // discarding is what the Discard dialog is asking about: {} for the whole job, or a Track.
  const [discarding, setDiscarding] = useState(null)
  const [busy, setBusy] = useState(false)
  const jobId = record?.id

  const show = useCallback((rev) => {
    setReview(rev)
    setValues(initialValues(rev))
  }, [])

  useEffect(() => {
    if (!jobId) return undefined
    let active = true
    httpClient(`${jobUrl(jobId)}/tags`)
      .then(({ json }) => active && show(json))
      .catch((error) => active && setLoadError(errorText(error)))
    return () => {
      active = false
    }
  }, [jobId, show])

  if (loadError) {
    return (
      <Typography color="error">
        {translate('resources.rip.review.loadError', { error: loadError })}
      </Typography>
    )
  }
  if (!review || !values) return null

  const edits = editsFrom(review, values)
  const invalid = invalidFields(values)
  const hasInvalid = Object.keys(invalid).length > 0

  const handleSave = () => {
    setSaving(true)
    httpClient(`${jobUrl(jobId)}/tags`, {
      method: 'PUT',
      body: JSON.stringify(edits),
    })
      .then(({ json }) => {
        show(json)
        notify('resources.rip.review.saved', 'info')
        refresh()
      })
      .catch((error) => notify(errorText(error), 'warning'))
      .finally(() => setSaving(false))
  }

  const handleDiscard = () => {
    const track = discarding?.track
    setBusy(true)
    httpClient(
      track ? `${jobUrl(jobId)}/items/${track.itemId}` : jobUrl(jobId),
      { method: 'DELETE' },
    )
      .then(() => {
        notify(
          track
            ? 'resources.rip.review.trackDiscarded'
            : 'resources.rip.review.discarded',
          'info',
        )
        if (track) {
          show({
            ...review,
            tracks: review.tracks.filter((t) => t.itemId !== track.itemId),
          })
        }
        setDiscarding(null)
        refresh()
      })
      .catch((error) => notify(errorText(error), 'warning'))
      .finally(() => setBusy(false))
  }

  const albumField = (field, label) => (
    <TextField
      id={`rip-${jobId}-${field}`}
      label={translate(label)}
      value={values[field]}
      onChange={(e) => setValues(setAlbumField(values, field, e.target.value))}
      error={!!invalid[field]}
      size="small"
      margin="dense"
      variant="outlined"
    />
  )

  const trackField = (track, field, label, width) => (
    <TextField
      inputProps={{
        'aria-label': `${translate(label)} ${track.position}`,
      }}
      value={values.tracks[track.itemId]?.[field] ?? ''}
      onChange={(e) =>
        setValues(setTrackField(values, track.itemId, field, e.target.value))
      }
      error={field === 'trackNumber' && !!invalid[track.itemId]}
      size="small"
      fullWidth={!width}
      style={width ? { width } : undefined}
    />
  )

  const discardMessage = discarding?.track
    ? translate('resources.rip.review.confirmTrack', {
        title: discarding.track.title || discarding.track.path,
      })
    : translate('resources.rip.review.confirmJob', {
        smart_count: review.tracks.filter((t) => t.path).length,
      })

  return (
    <Card variant="outlined">
      <CardContent>
        <Box display="flex" style={{ gap: 8 }} flexWrap="wrap">
          {albumField('album', 'resources.song.fields.album')}
          {albumField('albumArtist', 'resources.song.fields.albumArtist')}
          {albumField('year', 'resources.song.fields.year')}
        </Box>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell>
                {translate('resources.song.fields.trackNumber')}
              </TableCell>
              <TableCell>{translate('resources.song.fields.title')}</TableCell>
              <TableCell>{translate('resources.song.fields.artist')}</TableCell>
              <TableCell />
            </TableRow>
          </TableHead>
          <TableBody>
            {review.tracks.map((t) => (
              <TableRow key={t.itemId} data-testid={`track-${t.itemId}`}>
                {t.path ? (
                  <>
                    <TableCell>
                      {trackField(
                        t,
                        'trackNumber',
                        'resources.song.fields.trackNumber',
                        48,
                      )}
                    </TableCell>
                    <TableCell>
                      <Box display="flex" alignItems="center">
                        {trackField(t, 'title', 'resources.song.fields.title')}
                        {trackExtra && trackExtra(t)}
                      </Box>
                    </TableCell>
                    <TableCell>
                      {trackField(t, 'artist', 'resources.song.fields.artist')}
                    </TableCell>
                    <TableCell padding="none">
                      <Tooltip
                        title={translate('resources.rip.review.discardTrack')}
                      >
                        <IconButton
                          size="small"
                          aria-label={`${translate('resources.rip.review.discardTrack')} ${t.position}`}
                          onClick={() => setDiscarding({ track: t })}
                        >
                          <DeleteIcon fontSize="small" />
                        </IconButton>
                      </Tooltip>
                    </TableCell>
                  </>
                ) : (
                  <>
                    <TableCell>{t.position}</TableCell>
                    <TableCell colSpan={3}>
                      <Typography variant="body2" color="error">
                        {translate(`resources.rip.status.${t.status}`, {
                          _: t.status,
                        })}
                        {t.error && `: ${t.error}`}
                      </Typography>
                    </TableCell>
                  </>
                )}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
      <CardActions>
        <Button
          color="primary"
          variant="contained"
          onClick={handleSave}
          disabled={!edits || hasInvalid || saving}
        >
          {translate(saving ? 'resources.rip.review.saving' : 'ra.action.save')}
        </Button>
        {actions}
        <Button color="secondary" onClick={() => setDiscarding({})}>
          {translate('resources.rip.review.discard')}
        </Button>
      </CardActions>
      <DiscardDialog
        open={!!discarding}
        message={discardMessage}
        busy={busy}
        onConfirm={handleDiscard}
        onCancel={() => setDiscarding(null)}
      />
    </Card>
  )
}

export default ReviewCard
