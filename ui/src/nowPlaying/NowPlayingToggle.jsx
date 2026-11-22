import React from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { useTranslate } from 'react-admin'
import { IconButton } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import clsx from 'clsx'
import { MdCloseFullscreen, MdOpenInFull } from 'react-icons/md'
import { isNowPlayingOpen, toggleNowPlaying } from './nowPlayingState'

const useStyles = makeStyles(
  (theme) => ({
    // Over the player bar's own colour for its icons
    open: { '&& svg': { color: theme.palette.primary.main } },
  }),
  { name: 'NDNowPlayingToggle' },
)

// NowPlayingToggle is the player bar's button that opens and closes Now
// Playing on desktop, as its cover does.
const NowPlayingToggle = ({ className }) => {
  const translate = useTranslate()
  const dispatch = useDispatch()
  const open = useSelector(isNowPlayingOpen)
  const classes = useStyles()
  return (
    <IconButton
      size="small"
      className={clsx(className, open && classes.open)}
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
