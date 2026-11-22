import React, { useState } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { useTranslate } from 'react-admin'
import SpeedIcon from '@material-ui/icons/Speed'
import { NowPlayingToolButton } from '../nowPlaying/NowPlayingToolbar'
import { setPlaybackSpeed } from '../actions'
import { SPEEDS, formatSpeed, selectPlaybackSpeed } from './playbackSpeed'
import ToolSheet from './ToolSheet'

// SpeedTool is the toolbar's playback Speed: it shows the speed when it isn't
// normal, and opens a sheet to choose one. The player applies it.
const SpeedTool = () => {
  const translate = useTranslate()
  const dispatch = useDispatch()
  const speed = useSelector(selectPlaybackSpeed)
  const [open, setOpen] = useState(false)
  const changed = speed !== 1

  return (
    <>
      <NowPlayingToolButton
        icon={<SpeedIcon />}
        label={translate('nowPlaying.speed.label')}
        value={changed ? formatSpeed(speed) : undefined}
        active={changed}
        aria-haspopup="true"
        onClick={() => setOpen(true)}
      />
      <ToolSheet
        open={open}
        onClose={() => setOpen(false)}
        title={translate('nowPlaying.speed.title')}
        choices={SPEEDS.map((value) => ({ value, label: formatSpeed(value) }))}
        selected={speed}
        onChoose={(value) => dispatch(setPlaybackSpeed(value))}
      />
    </>
  )
}

export default SpeedTool
