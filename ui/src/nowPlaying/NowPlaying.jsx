import React, { useCallback } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { useHistory } from 'react-router-dom'
import { useTranslate } from 'react-admin'
import { Dialog, IconButton, Slide } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import KeyboardArrowDownIcon from '@material-ui/icons/KeyboardArrowDown'
import { closeNowPlaying, isNowPlayingOpen } from './nowPlayingState'
import { useWideEnoughForPanel } from './useNowPlayingPanel'
import { useBackToClose } from './useBackToClose'
import { useSwipeDown } from './useSwipeDown'
import NowPlayingContent from './NowPlayingContent'
import NowPlayingPanel from './NowPlayingPanel'
import { nowPlayingBackground } from './vuPalette'

const useStyles = makeStyles(
  (theme) => {
    return {
      paper: {
        backgroundColor: theme.palette.background.default,
        backgroundImage: nowPlayingBackground(theme),
        border: 'none',
        boxShadow: 'none',
        // Follows a down drag; springs back if it doesn't close
        transition: 'transform 0.2s ease-out',
      },
      scroller: {
        height: '100%',
        overflowY: 'auto',
        overscrollBehavior: 'contain',
      },
    }
  },
  { name: 'NDNowPlaying' },
)

const SlideUp = React.forwardRef((props, ref) => (
  <Slide direction="up" ref={ref} {...props} />
))
SlideUp.displayName = 'SlideUp'

// FullScreen is Now Playing on phones and narrow windows. The player package
// keeps playing underneath, so closing it, by its button, a down gesture, Esc
// or back, never touches playback.
const FullScreen = ({ audioInstance }) => {
  const translate = useTranslate()
  const dispatch = useDispatch()
  const router = useHistory()
  const classes = useStyles()
  const open = useSelector(isNowPlayingOpen)
  const close = useCallback(() => dispatch(closeNowPlaying()), [dispatch])
  const leave = useBackToClose(open, close)
  const { sheetRef, handlers } = useSwipeDown(close)

  const navigate = useCallback(
    (path) => {
      leave(path, router)
      close()
    },
    [leave, router, close],
  )

  return (
    <Dialog
      fullScreen
      open={open}
      onClose={close}
      TransitionComponent={SlideUp}
      PaperProps={{
        'aria-label': translate('nowPlaying.title'),
        'aria-modal': true,
        ref: sheetRef,
        className: classes.paper,
      }}
    >
      <div className={classes.scroller} {...handlers}>
        <NowPlayingContent
          audioInstance={audioInstance}
          onNavigate={navigate}
          closeButton={
            <IconButton
              aria-label={translate('nowPlaying.close')}
              onClick={close}
            >
              <KeyboardArrowDownIcon />
            </IconButton>
          }
        />
      </div>
    </Dialog>
  )
}

// NowPlaying is the Listener's own view of what they are playing: a side panel
// on a wide window, full screen otherwise. Resizing across PANEL_QUERY swaps
// one for the other, keeping it open or closed.
const NowPlaying = ({ audioInstance }) => {
  const open = useSelector(isNowPlayingOpen)
  const wide = useWideEnoughForPanel()
  if (!wide) return <FullScreen audioInstance={audioInstance} />
  return open ? <NowPlayingPanel audioInstance={audioInstance} /> : null
}

export default NowPlaying
