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

// The page may only narrow below what is on it where the browser has :has,
// which picks out the list cards that hold a table
const SUPPORTS_HAS = '@supports selector(:has(*))'

const useStyles = makeStyles({
  root: {
    // Room for the player bar, or the mini player on a phone
    paddingBottom: (props) => props.playerRoom,
    // Room for the Now Playing panel, so it sits beside the page, not over it
    paddingRight: (props) => (props.nowPlayingPanel ? PANEL_WIDTH : 0),
    // Within react-admin's width: 100%, so the page narrows rather than
    // running on under the panel
    boxSizing: 'border-box',
    // react-admin's own min-width: fit-content lets anything too wide on the
    // page, such as a table with long URLs, widen the whole layout and run it
    // on under the panel; capped at the window, the page narrows instead.
    // Only where a list's table can then scroll in its card (see content):
    // without :has, the page widens as before, rather than a wide table
    // running on under the panel with no way to scroll it out
    [SUPPORTS_HAS]: {
      minWidth: 0,
    },
    // react-admin's floating Create button (phones only) sits above the
    // mini player and the tab bar, not behind them
    '& [class*="RaCreateButton-floating"]': {
      bottom: (props) => `calc(${props.playerRoom || '0px'} + 16px)`,
    },
  },
  // The page's content area may shrink below the width of what is in it (a
  // flex item's min-width is otherwise its content's), so a wide table must
  // fit or scroll within it rather than widen the page
  content: {
    [SUPPORTS_HAS]: {
      minWidth: 0,
      // A list's card shrinks with the page too, and a table too wide for it
      // (Songs or Libraries beside the panel, say) scrolls sideways inside
      // the card rather than running on past the page's edge. Only a table's
      // card scrolls: a grid of album covers keeps its visible overflow,
      // which some themes rely on
      '& [class*="RaList-content"]': {
        minWidth: 0,
      },
      '& [class*="RaList-content"]:has(table)': {
        overflowX: 'auto',
      },
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
        classes={{ content: classes.content }}
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
