import React, { useCallback } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { Layout as RALayout, Sidebar, toggleSidebar } from 'react-admin'
import { useMediaQuery } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import { HotKeys } from 'react-hotkeys'
import Menu from './Menu'
import AppBar from './AppBar'
import Notification from './Notification'
import TabBar from './TabBar'
import useCurrentTheme from '../themes/useCurrentTheme'
import { useSearchRefocus } from '../common'
import { useResumeDownloads } from '../download'
import { OfflineGate } from '../download/OfflineGate'
import { PANEL_WIDTH } from '../nowPlaying/nowPlayingState'
import { useNowPlayingPanelShown } from '../nowPlaying/useNowPlayingPanel'
import { usePlayerRoom } from '../audioplayer/usePlayerRoom'

const useStyles = makeStyles({
  root: {
    // Room for the player bar, or the mini player on a phone
    paddingBottom: (props) => props.playerRoom,
    // Room for the Now Playing panel, so it sits beside the page, not over it
    paddingRight: (props) => (props.nowPlayingPanel ? PANEL_WIDTH : 0),
    // Within react-admin's width: 100%, so the page narrows rather than
    // running on under the panel
    boxSizing: 'border-box',
    // react-admin's floating Create button (phones only) sits above the
    // mini player and the tab bar, not behind them
    '& [class*="RaCreateButton-floating"]': {
      bottom: (props) => `calc(${props.playerRoom || '0px'} + 16px)`,
    },
  },
})

// On a phone the tab bar is the navigation, in place of the side drawer
const NoSidebar = () => null

const Layout = (props) => {
  // As the player: a phone below 810px, desktop from there up
  const isDesktop = useMediaQuery('(min-width:810px)')
  const theme = useCurrentTheme()
  const queue = useSelector((state) => state.player?.queue)
  const nowPlayingPanel = useNowPlayingPanelShown()
  const classes = useStyles({
    playerRoom: usePlayerRoom(),
    nowPlayingPanel: nowPlayingPanel && queue.length > 0,
  })
  const dispatch = useDispatch()
  useSearchRefocus()
  useResumeDownloads()

  const keyHandlers = {
    TOGGLE_MENU: useCallback(() => dispatch(toggleSidebar()), [dispatch]),
  }

  return (
    <HotKeys handlers={keyHandlers}>
      <RALayout
        {...props}
        className={classes.root}
        menu={Menu}
        sidebar={isDesktop ? Sidebar : NoSidebar}
        appBar={AppBar}
        theme={theme}
        notification={Notification}
      >
        <OfflineGate>{props.children}</OfflineGate>
        {!isDesktop && <TabBar />}
      </RALayout>
    </HotKeys>
  )
}

export default Layout
