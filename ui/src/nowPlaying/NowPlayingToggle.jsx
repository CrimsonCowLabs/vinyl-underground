import React from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { useTranslate } from 'react-admin'
import { IconButton } from '@material-ui/core'
import { MdCloseFullscreen, MdOpenInFull } from 'react-icons/md'
import { isNowPlayingOpen, toggleNowPlaying } from './nowPlayingState'

// NowPlayingToggle is the player bar's button that opens and closes Now
// Playing on desktop, as its cover does.
const NowPlayingToggle = ({ className }) => {
  const translate = useTranslate()
  const dispatch = useDispatch()
  const open = useSelector(isNowPlayingOpen)
  return (
    <IconButton
      size="small"
      className={className}
      color={open ? 'primary' : 'default'}
      aria-pressed={open}
      aria-label={translate(open ? 'nowPlaying.close' : 'nowPlaying.open')}
      onClick={(e) => {
        e.stopPropagation()
        dispatch(toggleNowPlaying())
      }}
    >
      {open ? <MdCloseFullscreen /> : <MdOpenInFull />}
    </IconButton>
  )
}

export default NowPlayingToggle
