import React from 'react'
import PropTypes from 'prop-types'
import { useTranslate } from 'react-admin'
import { CircularProgress, Tooltip } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import CheckIcon from '@material-ui/icons/Check'
import WarningIcon from '@material-ui/icons/Warning'
import clsx from 'clsx'
import { downloadsSupported, useDownload } from './index'

// A dark disc with a light ring and a white icon reads on light and dark
// themes alike, and on covers of any colour
const useStyles = makeStyles(
  {
    badge: {
      display: 'inline-flex',
      alignItems: 'center',
      justifyContent: 'center',
      boxSizing: 'border-box',
      width: 28,
      height: 28,
      borderRadius: '50%',
      color: '#fff',
      backgroundColor: 'rgba(0, 0, 0, 0.7)',
      boxShadow: '0 0 0 1px rgba(255, 255, 255, 0.6)',
    },
    icon: { fontSize: 18 },
  },
  { name: 'VUDownloadBadge' },
)

const iconFor = (download, classes) => {
  switch (download.state) {
    case 'downloading':
      return (
        <CircularProgress
          data-testid="download-badge-downloading"
          color="inherit"
          size={16}
          thickness={6}
          variant="determinate"
          value={(100 * download.done) / Math.max(download.total, 1)}
        />
      )
    case 'interrupted':
      return (
        <WarningIcon
          data-testid="download-badge-interrupted"
          className={classes.icon}
        />
      )
    default:
      return (
        <CheckIcon
          data-testid="download-badge-downloaded"
          className={classes.icon}
        />
      )
  }
}

const labelKeyFor = (download) => {
  switch (download.state) {
    case 'downloading':
      return 'download.badge.downloading'
    case 'interrupted':
      return `download.badge.interrupted.${download.kind}`
    default:
      return 'download.badge.downloaded'
  }
}

// Shows whether a Playlist or album has a Download on this device for the
// current Listener, and how far it has got: downloaded, downloading or
// interrupted. Shows nothing without one, or where Downloads can't be stored.
// It does nothing when clicked, so a click goes to whatever it sits on (e.g. a
// cover linking to its album).
export const DownloadBadge = ({ id, kind, className }) => {
  const translate = useTranslate()
  const classes = useStyles()
  const download = useDownload(id, kind)

  if (!downloadsSupported() || !download) return null

  const label = translate(labelKeyFor(download), download)
  return (
    <Tooltip title={label}>
      <span
        role="img"
        aria-label={label}
        className={clsx(classes.badge, className)}
      >
        {iconFor(download, classes)}
      </span>
    </Tooltip>
  )
}

DownloadBadge.propTypes = {
  id: PropTypes.string.isRequired,
  kind: PropTypes.oneOf(['playlist', 'album']).isRequired,
  className: PropTypes.string,
}
