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
import { makeStyles } from '@material-ui/core/styles'
import DeleteIcon from '@material-ui/icons/Delete'
import { httpClient } from '../../dataProvider'
import { DiscardDialog, DiscardJobButton } from './DiscardDialog'
import { useDiscardStyles } from './discardStyles'
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
import ReviewCover from './ReviewCover'
import {
  clearDraft,
  getDraft,
  pendingSave,
  saveTags,
  setDraft,
} from './reviewDrafts'

// PROPOSALS_POLL_MS is how often the card looks again while proposals are being worked out.
export const PROPOSALS_POLL_MS = 3000

// The Track table's columns. The table lays them out at these widths whatever is in them, so the
// card fits the room it has (beside the Now Playing panel, say) and its fields shrink to suit,
// rather than a long title widening the card and the job table it sits in. Title and Artist share
// what the number and the bin leave.
const COLUMNS = {
  trackNumber: '4.5em',
  title: '60%',
  artist: '40%',
  discard: '2.75em',
}

const useStyles = makeStyles(
  (theme) => ({
    // A long playlist's card scrolls within itself, so its footer stays in view. It can't stick
    // to the bottom of the window instead: the job table's card scrolls sideways (see Layout),
    // which would hold it there.
    card: {
      display: 'flex',
      flexDirection: 'column',
      maxHeight: 'max(320px, calc(100vh - 200px))',
    },
    content: {
      flex: '1 1 auto',
      minHeight: 0,
      overflowY: 'auto',
    },
    // Album, Album artist and Year in one row, while there is room for them
    header: {
      display: 'flex',
      flexWrap: 'wrap',
      alignItems: 'flex-start',
      gap: theme.spacing(1),
      marginBottom: theme.spacing(1),
    },
    albumField: { flex: '2 1 12em', minWidth: 0 },
    yearField: { flex: '0 1 7em', minWidth: '5em' },
    tracks: {
      tableLayout: 'fixed',
      width: '100%',
      '& th, & td': {
        padding: theme.spacing(0.5),
        verticalAlign: 'top',
      },
      '& th': { paddingTop: theme.spacing(1) },
    },
    // A failed Track's reason, which a fixed column would otherwise run on past
    failed: { overflowWrap: 'anywhere' },
    // A proposal under a field: small, and on one line, cut short if it is too long, so a Track
    // with proposals is no taller than one without
    hint: {
      fontSize: '0.6875rem',
      lineHeight: '1rem',
      height: '1rem',
      margin: '2px 4px 0',
      whiteSpace: 'nowrap',
      overflow: 'hidden',
      textOverflow: 'ellipsis',
    },
    hintLink: {
      display: 'block',
      maxWidth: '100%',
      overflow: 'hidden',
      textOverflow: 'ellipsis',
      whiteSpace: 'nowrap',
      textAlign: 'left',
    },
    footer: {
      flexShrink: 0,
      gap: theme.spacing(1),
      borderTop: `1px solid ${theme.palette.divider}`,
      padding: theme.spacing(1, 2),
    },
    // Discard sits apart from the actions that keep the job
    discard: { marginLeft: 'auto' },
  }),
  { name: 'NDReviewCard' },
)

// ReviewCard is a finished job's review card: its album-level tags and its Track list, read from
// the Files, to edit and save into them, and Discard for the whole job or one Track. Only the
// admin sees Rips at all. Save, the actions and Discard sit in its footer.
//
// Slots for later work: trackExtra(track) renders under a Track's title (e.g. #32's "already
// have" indicator), and actions are more buttons beside Save and Discard: an element, or a
// function given { jobId, review, dirty (unsaved edits), show(review) } (e.g. #36's "Promote
// all").
//
// Proposed tags (#34) are offered under each field ("Proposed: X", click to take it), or all at
// once with "Use proposals". A field holding a proposal the File doesn't have yet says so. They
// only go into the form (see reviewForm.js): nothing is written until Save.
//
// Unsaved edits are kept per job outside the card (see reviewDrafts.js), as the Rip list remounts
// the card each time it refreshes: a remounted card picks up where the admin left off.
const ReviewCard = ({ record, trackExtra, actions }) => {
  const translate = useTranslate()
  const notify = useNotify()
  const refresh = useRefresh()
  const classes = useStyles()
  const discardClasses = useDiscardStyles()
  const jobId = record?.id
  // draft is the job's unsaved edits from before the card was last remounted, if it has any.
  const [draft] = useState(() => getDraft(jobId))
  const [review, setReview] = useState(draft?.review ?? null)
  const [values, setValues] = useState(draft?.values ?? null)
  const [loadError, setLoadError] = useState(null)
  const [saving, setSaving] = useState(false)
  // discarding is the Track the Discard dialog is asking about.
  const [discarding, setDiscarding] = useState(null)
  const [busy, setBusy] = useState(false)
  // loadedAt is when the card last took in its review from the server, which the cover's URL
  // carries so it is fetched afresh each time (see ReviewCover).
  const [loadedAt, setLoadedAt] = useState(() => Date.now())

  const show = useCallback((rev) => {
    setReview(rev)
    setValues(initialValues(rev))
    setLoadedAt(Date.now())
  }, [])

  useEffect(() => {
    if (!jobId || draft) return undefined
    let active = true
    httpClient(`${jobUrl(jobId)}/tags`)
      .then(({ json }) => active && show(json))
      .catch((error) => active && setLoadError(errorText(error)))
    return () => {
      active = false
    }
  }, [jobId, draft, show])

  // A Save made before the card was remounted shows here once it is done.
  useEffect(() => {
    const save = pendingSave(jobId)
    if (!save) return undefined
    let active = true
    setSaving(true)
    save
      .then((json) => active && show(json))
      .catch(() => {}) // The card that saved says why.
      .finally(() => active && setSaving(false))
    return () => {
      active = false
    }
  }, [jobId, show])

  // Keep the job's draft in step with the form: while there are unsaved edits, they are kept for a
  // remounted card; once there are none (saved, or undone), it goes.
  useEffect(() => {
    if (!jobId || !review || !values) return
    if (editsFrom(review, values)) setDraft(jobId, { review, values })
    else clearDraft(jobId)
  }, [jobId, review, values])

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
      <Box
        display="flex"
        alignItems="center"
        flexWrap="wrap"
        style={{ gap: 8 }}
      >
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
  // A card with proposals keeps a line under every Track field for one, so each row is as tall
  // as the next whichever fields have proposals.
  const hintLine =
    !!review.proposalsPending || review.tracks.some((t) => t.proposal)

  const handleRefreshProposals = () =>
    httpClient(`${jobUrl(jobId)}/proposals`, { method: 'POST' })
      .then(({ json }) => {
        // The cover comes along too: it may have changed since the card was loaded.
        setReview((r) => ({ ...withProposalsFrom(r, json), cover: json.cover }))
        setLoadedAt(Date.now())
      })
      .catch((error) => notify(errorText(error), 'warning'))

  // proposalHint is a field's helper text: the proposal to take, or a note that the field holds
  // one the File doesn't have yet.
  const proposalHint = (fileValue, value, proposedValue, take) => {
    const hint = fieldHint(fileValue, value, proposedValue)
    if (!hint) return undefined
    if (hint.offer) {
      const text = translate('resources.rip.review.proposed', {
        value: hint.offer,
      })
      return (
        <Link
          component="button"
          type="button"
          variant="inherit"
          className={classes.hintLink}
          title={text}
          onClick={take}
        >
          {text}
        </Link>
      )
    }
    const text = hint.file
      ? translate('resources.rip.review.proposedFile', { file: hint.file })
      : translate('resources.rip.review.proposedNew')
    return <span title={text}>{text}</span>
  }

  // Saving changes only the Files' tags, which the job list doesn't show, so the card takes in
  // the saved tags itself and the list is left alone (with any other card's unsaved edits).
  const handleSave = () => {
    setSaving(true)
    saveTags(jobId, edits)
      .then((json) => {
        show(json)
        notify('resources.rip.review.saved', 'info')
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
        // The job's Tracks changed: the list shows them. The edits left stay in the draft.
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
      FormHelperTextProps={{ className: classes.hint }}
      className={field === 'year' ? classes.yearField : classes.albumField}
      size="small"
      margin="dense"
      variant="outlined"
    />
  )

  const trackField = (track, field, label) => (
    <TextField
      inputProps={{
        'aria-label': `${translate(label)} ${track.position}`,
      }}
      value={values.tracks[track.itemId]?.[field] ?? ''}
      onChange={(e) =>
        setValues(setTrackField(values, track.itemId, field, e.target.value))
      }
      helperText={
        proposalHint(
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
        ) ?? (hintLine ? ' ' : undefined)
      }
      FormHelperTextProps={{ className: classes.hint }}
      error={field === 'trackNumber' && !!invalid[track.itemId]}
      size="small"
      variant="outlined"
      fullWidth
    />
  )

  const discardTrackButton = (t) => (
    <Tooltip title={translate('resources.rip.review.discardTrack')}>
      <IconButton
        size="small"
        className={discardClasses.icon}
        aria-label={`${translate('resources.rip.review.discardTrack')} ${t.position}`}
        onClick={() => setDiscarding(t)}
      >
        <DeleteIcon fontSize="small" />
      </IconButton>
    </Tooltip>
  )

  return (
    <Card variant="outlined" className={classes.card}>
      <CardContent className={classes.content}>
        <ProposalsBar
          review={review}
          proposed={proposed}
          onUse={() => setValues(applyProposals(values, proposed))}
          onRefresh={handleRefreshProposals}
        />
        <div className={classes.header} data-testid="review-header">
          <ReviewCover cover={review.cover} loadedAt={loadedAt} />
          {albumField('album', 'resources.song.fields.album')}
          {albumField('albumArtist', 'resources.song.fields.albumArtist')}
          {albumField('year', 'resources.song.fields.year')}
        </div>
        <Table
          size="small"
          className={classes.tracks}
          style={{ tableLayout: 'fixed' }}
          data-testid="review-tracks"
        >
          <colgroup>
            {Object.entries(COLUMNS).map(([column, width]) => (
              <col key={column} data-column={column} style={{ width }} />
            ))}
          </colgroup>
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
                      )}
                    </TableCell>
                    <TableCell>
                      {trackField(t, 'title', 'resources.song.fields.title')}
                      {trackExtra && trackExtra(t)}
                    </TableCell>
                    <TableCell>
                      {trackField(t, 'artist', 'resources.song.fields.artist')}
                    </TableCell>
                  </>
                ) : (
                  <>
                    <TableCell>{t.position}</TableCell>
                    <TableCell colSpan={2}>
                      <Typography
                        variant="body2"
                        color="error"
                        className={classes.failed}
                      >
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
      <CardActions
        disableSpacing
        className={classes.footer}
        data-testid="review-footer"
      >
        <Button
          color="primary"
          variant="contained"
          size="small"
          onClick={handleSave}
          disabled={!edits || hasInvalid || saving}
        >
          {translate(saving ? 'resources.rip.review.saving' : 'ra.action.save')}
        </Button>
        {typeof actions === 'function'
          ? actions({ jobId, review, dirty: !!edits, show })
          : actions}
        <span className={classes.discard}>
          <DiscardJobButton
            jobId={jobId}
            fileCount={review.tracks.filter((t) => t.path).length}
          />
        </span>
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
