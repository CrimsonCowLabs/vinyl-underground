import React from 'react'
import PropTypes from 'prop-types'
import { useTranslate } from 'react-admin'
import { Typography } from '@material-ui/core'
import { RippedBadge } from '../common/RippedBadge'

// trackLine is "<Artist> - <Title> · <Album> · <Library>", leaving out what the Track hasn't got.
const trackLine = (t) =>
  [t.artist ? `${t.artist} - ${t.title}` : t.title, t.album, t.libraryName]
    .filter(Boolean)
    .join(' · ')

// AlreadyHave warns that the Library (or the Inbox) already seems to have a video about to be
// ripped, listing the Tracks that look like it. It is only a warning: a likely match may be
// another recording, and it is never a Duplicate (identical Files). Renders nothing without
// matches.
export const AlreadyHave = ({ tracks }) => {
  const translate = useTranslate()
  if (!tracks?.length) {
    return null
  }
  return (
    <Typography
      variant="caption"
      color="textSecondary"
      component="div"
      data-testid="already-have"
    >
      {translate('resources.rip.messages.alreadyHave')}
      {tracks.map((t) => (
        <div key={t.id}>
          {trackLine(t)}
          <RippedBadge record={t} />
        </div>
      ))}
    </Typography>
  )
}

AlreadyHave.propTypes = {
  tracks: PropTypes.arrayOf(PropTypes.object),
}

export default AlreadyHave
