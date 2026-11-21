import React from 'react'
import { useTranslate } from 'react-admin'
import { Box, Button, CircularProgress, Typography } from '@material-ui/core'

// ProposalsBar heads a review card that has tag proposals (#34): whether they are still being
// worked out, any note about them (e.g. MusicBrainz couldn't be asked), "Use proposals" to put
// them all into the form, and "Refresh proposals" to work them out again. It shows nothing for a
// card without proposals.
const ProposalsBar = ({ review, proposed, onUse, onRefresh }) => {
  const translate = useTranslate()
  const tracks = review.tracks || []
  const hasAny = tracks.some((t) => t.proposal)
  if (!hasAny && !review.proposalsPending) return null

  const notes = [
    ...new Set(tracks.map((t) => t.proposal?.note).filter(Boolean)),
  ]
  const canUse =
    Object.keys(proposed).some((k) => k !== 'tracks') ||
    Object.keys(proposed.tracks).length > 0

  return (
    <Box
      display="flex"
      alignItems="center"
      flexWrap="wrap"
      style={{ gap: 8 }}
      mb={1}
    >
      <Typography variant="subtitle2">
        {translate('resources.rip.review.proposals')}
      </Typography>
      {review.proposalsPending && (
        <Box display="flex" alignItems="center" style={{ gap: 4 }}>
          <CircularProgress size={14} />
          <Typography variant="caption">
            {translate('resources.rip.review.proposalsPending')}
          </Typography>
        </Box>
      )}
      <Button size="small" onClick={onUse} disabled={!canUse}>
        {translate('resources.rip.review.useProposals')}
      </Button>
      {/* Not disabled while pending: a proposal stuck pending can always be asked for again. */}
      <Button size="small" onClick={onRefresh}>
        {translate('resources.rip.review.refreshProposals')}
      </Button>
      {notes.map((note) => (
        <Typography key={note} variant="caption" color="textSecondary">
          {note}
        </Typography>
      ))}
    </Box>
  )
}

export default ProposalsBar
