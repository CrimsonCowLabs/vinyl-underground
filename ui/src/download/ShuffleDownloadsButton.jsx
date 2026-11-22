import React from 'react'
import PropTypes from 'prop-types'
import { useDispatch } from 'react-redux'
import { useNotify, useTranslate } from 'react-admin'
import { Button, IconButton, Tooltip } from '@material-ui/core'
import ShuffleIcon from '@material-ui/icons/Shuffle'
import { shuffleTracks } from '../actions'
import { downloadedTracksForPlayer } from './index'

// Plays every Track downloaded on this device, shuffled: what the Listener
// can always play offline
export const ShuffleDownloadsButton = ({ compact }) => {
  const translate = useTranslate()
  const notify = useNotify()
  const dispatch = useDispatch()
  const label = translate('download.actions.shuffleAll')

  const shuffle = async () => {
    const tracks = await downloadedTracksForPlayer()
    if (Object.keys(tracks).length === 0) {
      notify('download.message.nothingToShuffle', 'info')
      return
    }
    dispatch(shuffleTracks(tracks))
  }

  if (compact) {
    return (
      <Tooltip title={label}>
        <IconButton color="inherit" aria-label={label} onClick={shuffle}>
          <ShuffleIcon />
        </IconButton>
      </Tooltip>
    )
  }
  // Labelled on a phone too, where react-admin's Button shows only its icon
  return (
    <Button
      onClick={shuffle}
      variant="contained"
      color="primary"
      startIcon={<ShuffleIcon />}
    >
      {label}
    </Button>
  )
}

ShuffleDownloadsButton.propTypes = {
  compact: PropTypes.bool,
}
