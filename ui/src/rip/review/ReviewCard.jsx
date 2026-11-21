import React, { useCallback, useEffect, useState } from 'react'
import { useNotify, useRefresh, useTranslate } from 'react-admin'
import {
  Box,
  Button,
  Card,
  CardActions,
  CardContent,
  IconButton,
  Link,
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
import { DiscardDialog, DiscardJobButton } from './DiscardDialog'
import { errorText, jobUrl } from './reviewApi'
import {
  applyProposals,
  editsFrom,
  fieldHint,
  initialValues,
  invalidFields,
  proposedValues,
  removeTrack,
  setAlbumField,
  setTrackField,
  withProposalsFrom,
} from './reviewForm'
import ProposalsBar from './ProposalsBar'

// PROPOSALS_POLL_MS is how often the card looks again while proposals are being worked out.
export const PROPOSALS_POLL_MS = 3000

// ReviewCard is a finished job's review card: its album-level tags and its Track list, read from
// the Files, to edit and save into them, and Discard for the whole job or one Track. Only the
// admin sees Rips at all.
//
// Slots for later work: trackExtra(track) renders next to a Track's title (e.g. #32's "already
// have" indicator), and actions are more buttons beside Save and Discard (e.g. #36's "Promote
// all").
//
// Proposed tags (#34) are offered under each field ("Proposed: X", click to take it), or all at
// once with "Use proposals". A field holding a proposal the File doesn't have yet says so. They
// only go into the form: nothing is written until Save.
const ReviewCard = ({ record, trackExtra, actions }) => {
  const translate = useTranslate()
  const notify = useNotify()
  const refresh = useRefresh()
  const [review, setReview] = useState(null)
  const [values, setValues] = useState(null)
  const [loadError, setLoadError] = useState(null)
  const [saving, setSaving] = useState(false)
  // discarding is the Track the Discard dialog is asking about.
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

  // While proposals are being worked out, look again now and then, taking in only the proposals
  // so the admin's edits stay.
  const pending = !!review?.proposalsPending
  // pollFailures makes a failed look go round again.
  const [pollFailures, setPollFailures] = useState(0)
  useEffect(() => {
    if (!jobId || !pending) return undefined
    let active = true
    const timer = setTimeout(() => {
      httpClient(`${jobUrl(jobId)}/tags`)
        .then(
          ({ json }) => active && setReview((r) => withProposalsFrom(r, json)),
        )
        .catch(() => active && setPollFailures((n) => n + 1))
    }, PROPOSALS_POLL_MS)
    return () => {
      active = false
      clearTimeout(timer)
    }
  }, [jobId, pending, review, pollFailures])

  if (loadError) {
    // Discard still works on a job whose tags can't be read.
    return (
      <Box display="flex" alignItems="center">
        <Typography color="error">
          {translate('resources.rip.review.loadError', { error: loadError })}
        </Typography>
        <DiscardJobButton
          jobId={jobId}
          fileCount={(record.items || []).filter((it) => it.path).length}
        />
      </Box>
    )
  }
  if (!review || !values) return null

  const edits = editsFrom(review, values)
  const invalid = invalidFields(values)
  const hasInvalid = Object.keys(invalid).length > 0
  const proposed = proposedValues(review)

  const handleRefreshProposals = () =>
    httpClient(`${jobUrl(jobId)}/proposals`, { method: 'POST' })
      .then(({ json }) => setReview((r) => withProposalsFrom(r, json)))
      .catch((error) => notify(errorText(error), 'warning'))

  // proposalHint is a field's helper text: the proposal to take, or a note that the field holds
  // one the File doesn't have yet.
  const proposalHint = (fileValue, value, proposedValue, take) => {
    const hint = fieldHint(fileValue, value, proposedValue)
    if (!hint) return undefined
    if (hint.offer) {
      return (
        <Link component="button" type="button" variant="caption" onClick={take}>
          {translate('resources.rip.review.proposed', { value: hint.offer })}
        </Link>
      )
    }
    return hint.file
      ? translate('resources.rip.review.proposedFile', { file: hint.file })
      : translate('resources.rip.review.proposedNew')
  }

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

  const handleDiscardTrack = () => {
    const track = discarding
    setBusy(true)
    httpClient(`${jobUrl(jobId)}/items/${track.itemId}`, { method: 'DELETE' })
      .then(() => {
        notify('resources.rip.review.trackDiscarded', 'info')
        // Keep the admin's unsaved edits to the other Tracks.
        setReview({
          ...review,
          tracks: review.tracks.filter((t) => t.itemId !== track.itemId),
        })
        setValues(removeTrack(values, track.itemId))
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
      helperText={proposalHint(
        review[field],
        values[field],
        proposed[field],
        () => setValues(setAlbumField(values, field, proposed[field])),
      )}
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
      helperText={proposalHint(
        track[field],
        values.tracks[track.itemId]?.[field],
        proposed.tracks[track.itemId]?.[field],
        () =>
          setValues(
            setTrackField(
              values,
              track.itemId,
              field,
              proposed.tracks[track.itemId][field],
            ),
          ),
      )}
      error={field === 'trackNumber' && !!invalid[track.itemId]}
      size="small"
      fullWidth={!width}
      style={width ? { width } : undefined}
    />
  )

  const discardTrackButton = (t) => (
    <Tooltip title={translate('resources.rip.review.discardTrack')}>
      <IconButton
        size="small"
        aria-label={`${translate('resources.rip.review.discardTrack')} ${t.position}`}
        onClick={() => setDiscarding(t)}
      >
        <DeleteIcon fontSize="small" />
      </IconButton>
    </Tooltip>
  )

  return (
    <Card variant="outlined">
      <CardContent>
        <ProposalsBar
          review={review}
          proposed={proposed}
          onUse={() => setValues(applyProposals(values, proposed))}
          onRefresh={handleRefreshProposals}
        />
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
                  </>
                ) : (
                  <>
                    <TableCell>{t.position}</TableCell>
                    <TableCell colSpan={2}>
                      <Typography variant="body2" color="error">
                        {translate(`resources.rip.status.${t.status}`, {
                          _: t.status,
                        })}
                        {t.error && `: ${t.error}`}
                      </Typography>
                    </TableCell>
                  </>
                )}
                <TableCell padding="none">{discardTrackButton(t)}</TableCell>
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
        <DiscardJobButton
          jobId={jobId}
          fileCount={review.tracks.filter((t) => t.path).length}
        />
      </CardActions>
      <DiscardDialog
        open={!!discarding}
        message={translate('resources.rip.review.confirmTrack', {
          title: discarding?.title || discarding?.path || discarding?.position,
        })}
        busy={busy}
        onConfirm={handleDiscardTrack}
        onCancel={() => setDiscarding(null)}
      />
    </Card>
  )
}

export default ReviewCard
