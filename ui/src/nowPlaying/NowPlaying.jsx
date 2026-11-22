import React, { useCallback } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { useHistory } from 'react-router-dom'
import { useTranslate } from 'react-admin'
import { Dialog, IconButton, Slide } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import KeyboardArrowDownIcon from '@material-ui/icons/KeyboardArrowDown'
import { closeNowPlaying } from './nowPlayingState'
import { useBackToClose } from './useBackToClose'
import { useSwipeDown } from './useSwipeDown'
import NowPlayingContent from './NowPlayingContent'
import { vuPalette } from './vuPalette'

const useStyles = makeStyles(
  (theme) => {
    const vu = vuPalette(theme)
    return {
      paper: {
        backgroundColor: theme.palette.background.default,
        backgroundImage: `linear-gradient(180deg, ${vu.accentGlow}, transparent 58%)${
          vu.grain ? `, ${vu.grain}` : ''
        }`,
        border: 'none',
        boxShadow: 'none',
        // Follows a down drag; springs back if it doesn't close
        transition: 'transform 0.2s ease-out',
      },
      dragging: { transition: 'none' },
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

// NowPlaying is the Listener's own full-screen view of what they are playing,
// on phones. The player package keeps playing underneath, so closing it, by
// its button, a down gesture, Esc or back, never touches playback.
const NowPlaying = ({ audioInstance }) => {
  const translate = useTranslate()
  const dispatch = useDispatch()
  const router = useHistory()
  const classes = useStyles()
  const open = useSelector((state) => !!state.nowPlaying?.open)
  const close = useCallback(() => dispatch(closeNowPlaying()), [dispatch])
  const leave = useBackToClose(open, close)
  const { offset, handlers } = useSwipeDown(close)

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
        className: offset
          ? `${classes.paper} ${classes.dragging}`
          : classes.paper,
        style: offset ? { transform: `translateY(${offset}px)` } : undefined,
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

export default NowPlaying
