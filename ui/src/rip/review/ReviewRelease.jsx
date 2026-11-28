import React, { useEffect, useRef, useState } from 'react'
import PropTypes from 'prop-types'
import { useNotify, useTranslate } from 'react-admin'
import { Box, Button, Tooltip, Typography } from '@material-ui/core'
import config from '../../config'
import { httpClient } from '../../dataProvider'
import { ReleaseLookup } from '../../album/ReleaseLookup'
import { useReleaseLookup } from '../../album/useReleaseLookup'
import { jobUrl } from './reviewApi'

// The server's refusals the card has words for; anything else is unknown.
const knownErrors = [
  'promoting',
  'editingOff',
  'changedOnDisk',
  'unsupportedFormat',
  'sharedFolder',
  'notFound',
  'busy',
  'unavailable',
  'disabled',
]

// cardAlbum is the album a card's Tracks are all on, or null when they are on several, or some
// aren't scanned yet (they have no album to look up).
const cardAlbum = (review) => {
  const files = (review?.tracks || []).filter((t) => t.path)
  const ids = new Set(files.map((t) => t.albumId))
  if (files.length === 0 || ids.size !== 1 || !files[0].albumId) return null
  return {
    id: files[0].albumId,
    trackCount: files.length,
    artist: review.albumArtist || files[0].artist || '',
    title: review.album || '',
    compilation: false,
  }
}

const ReleaseSection = ({
  jobId,
  album,
  dirty,
  held,
  writing,
  onHold,
  onApplied,
  onWriting,
}) => {
  const translate = useTranslate()
  const notify = useNotify()
  const lookup = useReleaseLookup(album)
  const [applying, setApplying] = useState(false)
  const mounted = useRef(true)
  useEffect(
    () => () => {
      mounted.current = false
    },
    [],
  )

  // A release picked, or being applied, holds Promote off the card.
  const underway = !!lookup.release || applying
  useEffect(() => {
    onHold(underway)
  }, [underway, onHold])
  useEffect(() => () => onHold(false), [onHold])

  // The release goes into the Files as they are, which the card's unsaved edits aren't: it sends
  // none of the card's values, so the album keeps its own but for what the admin ticks.
  const handleApply = () => {
    setApplying(true)
    lookup
      .apply({})
      .then(({ done, jobId: editJobId, note, notifyOptions }) => {
        notify(
          note ||
            (done
              ? 'resources.rip.review.release.applied'
              : 'resources.rip.review.release.background'),
          notifyOptions,
        )
        lookup.pick(null)
        // Still being written: Promote waits for it (see useReleaseWrite).
        if (!done && editJobId) onWriting(editJobId)
        // The album's ID follows its tags: the card takes it, and the rest, from its review. A
        // reload that fails leaves the card as it was, the release applied all the same.
        return httpClient(`${jobUrl(jobId)}/tags`)
          .then(({ json }) => mounted.current && onApplied(json))
          .catch(() =>
            notify('resources.rip.review.release.reloadFailed', {
              type: 'warning',
            }),
          )
      })
      .catch((err) => {
        const code = err?.body?.error
        notify(
          `resources.rip.review.release.errors.${knownErrors.includes(code) ? code : 'unknown'}`,
          { type: 'warning' },
        )
      })
      .finally(() => mounted.current && setApplying(false))
  }

  const blocked = dirty || held || writing || applying
  const button = (
    <Button
      color="primary"
      variant="outlined"
      size="small"
      onClick={handleApply}
      disabled={!lookup.paired || blocked}
    >
      {translate(
        applying
          ? 'resources.rip.review.release.applying'
          : 'resources.rip.review.release.apply',
      )}
    </Button>
  )
  return (
    <Box mt={1} data-testid="review-release">
      <ReleaseLookup lookup={lookup} disabled={blocked} />
      {writing && (
        <Typography variant="caption" display="block" color="textSecondary">
          {translate('resources.rip.review.release.writing')}
        </Typography>
      )}
      {lookup.release && (
        <Box mt={1}>
          <Typography
            variant="caption"
            display="block"
            color="textSecondary"
            gutterBottom
          >
            {translate('resources.rip.review.release.stillInInbox')}
          </Typography>
          {dirty ? (
            <Tooltip title={translate('resources.rip.review.release.unsaved')}>
              {/* A disabled button gets no hover events: the span does. */}
              <span>{button}</span>
            </Tooltip>
          ) : (
            button
          )}
        </Box>
      )}
    </Box>
  )
}

// ReviewRelease is Find on MusicBrainz on a review card whose Tracks are one album (#219): the
// album Edit dialog's release flow (see useReleaseLookup), searched by the card's album, album
// artist and Track count. Applying writes the release's tags, MusicBrainz IDs, genres and cover
// into the Inbox Files as one edit job, undoable from the Edit history, then reloads the card
// (onApplied). It waits for unsaved edits (dirty) to be saved, for Promote (held), and for a
// release still being written in the background (writing, which onWriting starts); while a
// release is picked or applied it holds Promote off (onHold). It isn't there while editing is off
// (as Promote isn't: the Inbox is written only while editing is on).
const ReviewRelease = ({ review, ...rest }) => {
  const album = cardAlbum(review)
  if (!config.enablePromote || !album) return null
  return <ReleaseSection key={album.id} album={album} {...rest} />
}

ReviewRelease.propTypes = {
  jobId: PropTypes.string.isRequired,
  review: PropTypes.object.isRequired,
  dirty: PropTypes.bool,
  held: PropTypes.bool,
  writing: PropTypes.bool,
  onHold: PropTypes.func.isRequired,
  onApplied: PropTypes.func.isRequired,
  onWriting: PropTypes.func.isRequired,
}

export default ReviewRelease
