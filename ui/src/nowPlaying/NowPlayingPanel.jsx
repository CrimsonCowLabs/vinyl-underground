import React, { useCallback } from 'react'
import { useDispatch } from 'react-redux'
import { useHistory } from 'react-router-dom'
import { useTranslate } from 'react-admin'
import { IconButton } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import CloseIcon from '@material-ui/icons/Close'
import { closeNowPlaying, PANEL_WIDTH } from './nowPlayingState'
import NowPlayingContent from './NowPlayingContent'
import { vuPalette } from './vuPalette'

// The app bar's height on desktop (react-admin's dense toolbar), and the
// player bar's, which the panel sits between
const APP_BAR_HEIGHT = 48
const PLAYER_BAR_HEIGHT = 80

const useStyles = makeStyles(
  (theme) => {
    const vu = vuPalette(theme)
    return {
      root: {
        position: 'fixed',
        top: APP_BAR_HEIGHT,
        right: 0,
        bottom: PLAYER_BAR_HEIGHT,
        width: PANEL_WIDTH,
        boxSizing: 'border-box',
        zIndex: theme.zIndex.appBar - 1,
        overflowY: 'auto',
        overscrollBehavior: 'contain',
        // The art sizes itself to the panel rather than the window
        containerType: 'inline-size',
        borderLeft: `1px solid ${theme.palette.divider}`,
        backgroundColor: theme.palette.background.paper,
        backgroundImage: `linear-gradient(180deg, ${vu.accentGlow}, transparent 58%)${
          vu.grain ? `, ${vu.grain}` : ''
        }`,
      },
    }
  },
  { name: 'NDNowPlayingPanel' },
)

// NowPlayingPanel is Now Playing on a wide window: docked on the right, next
// to the page rather than over it (Layout makes room for it), and left open
// as the Listener moves around the app.
const NowPlayingPanel = ({ audioInstance }) => {
  const translate = useTranslate()
  const dispatch = useDispatch()
  const router = useHistory()
  const classes = useStyles()
  const close = useCallback(() => dispatch(closeNowPlaying()), [dispatch])
  const navigate = useCallback(
    (path) => {
      if (path !== router.location.pathname) router.push(path)
    },
    [router],
  )

  return (
    <aside aria-label={translate('nowPlaying.title')} className={classes.root}>
      <NowPlayingContent
        audioInstance={audioInstance}
        onNavigate={navigate}
        closeButton={
          <IconButton
            aria-label={translate('nowPlaying.close')}
            onClick={close}
          >
            <CloseIcon />
          </IconButton>
        }
      />
    </aside>
  )
}

export default NowPlayingPanel
