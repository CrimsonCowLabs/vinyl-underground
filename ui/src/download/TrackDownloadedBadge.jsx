import React from 'react'
import PropTypes from 'prop-types'
import { useTranslate } from 'react-admin'
import { Tooltip } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import OfflinePinIcon from '@material-ui/icons/OfflinePin'
import clsx from 'clsx'
import { trackIdOf } from './downloads'
import { downloadsSupported, useTrackStored } from './index'

const useStyles = makeStyles(
  (theme) => ({
    badge: {
      display: 'inline-flex',
      marginLeft: theme.spacing(1),
      verticalAlign: 'text-bottom',
      color: theme.palette.text.secondary,
      cursor: 'default',
    },
    icon: { fontSize: '1.1em' },
  }),
  { name: 'VUTrackDownloadedBadge' },
)

// Marks a song whose Track is stored on this device, whichever of the current
// Listener's album or Playlist Downloads brought it there, so the Listener
// knows it plays offline. Shows nothing for any other song, or where Downloads
// can't be stored.
export const TrackDownloadedBadge = ({ record, className }) => {
  const translate = useTranslate()
  const classes = useStyles()
  const stored = useTrackStored(record ? trackIdOf(record) : undefined)

  if (!downloadsSupported() || !stored) return null

  return (
    <Tooltip title={translate('resources.song.downloaded.tooltip')}>
      <span
        role="img"
        aria-label={translate('resources.song.downloaded.label')}
        className={clsx(classes.badge, className)}
      >
        <OfflinePinIcon className={classes.icon} />
      </span>
    </Tooltip>
  )
}

TrackDownloadedBadge.propTypes = {
  record: PropTypes.object,
  className: PropTypes.string,
}
