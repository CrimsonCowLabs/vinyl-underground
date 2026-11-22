import React, { useCallback } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { Layout as RALayout, toggleSidebar } from 'react-admin'
import { makeStyles } from '@material-ui/core/styles'
import { HotKeys } from 'react-hotkeys'
import Menu from './Menu'
import AppBar from './AppBar'
import Notification from './Notification'
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
  },
})

const Layout = (props) => {
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
        appBar={AppBar}
        theme={theme}
        notification={Notification}
      >
        <OfflineGate>{props.children}</OfflineGate>
      </RALayout>
    </HotKeys>
  )
}

export default Layout
