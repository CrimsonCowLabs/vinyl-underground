import React, { useState } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { useTranslate } from 'react-admin'
import { makeStyles } from '@material-ui/core/styles'
import clsx from 'clsx'
import LibraryMusicIcon from '@material-ui/icons/LibraryMusic'
import QueueMusicIcon from '@material-ui/icons/QueueMusic'
import HeadsetIcon from '@material-ui/icons/Headset'
import GetAppIcon from '@material-ui/icons/GetApp'
import MoreHorizIcon from '@material-ui/icons/MoreHoriz'
import albumLists, { defaultAlbumList } from '../album/albumLists'
import { getStoredDefaultView } from '../personal/defaultViews'
import { vuPalette } from '../nowPlaying/vuPalette'
import {
  downloadsSupported,
  isAvailableOffline,
  useIsOffline,
} from '../download'
import { sectionOf } from '../download/offline'
import MoreSheet from './MoreSheet'
import { TAB_BAR_HEIGHT } from './tabBarRoom'

const useStyles = makeStyles(
  (theme) => {
    const vu = vuPalette(theme)
    return {
      root: {
        position: 'fixed',
        left: 0,
        right: 0,
        bottom: 0,
        zIndex: theme.zIndex.appBar,
        display: 'grid',
        gridAutoFlow: 'column',
        gridAutoColumns: '1fr',
        boxSizing: 'content-box',
        height: TAB_BAR_HEIGHT - 12,
        padding: '6px 4px',
        // Clear of the home indicator
        paddingBottom: 'calc(env(safe-area-inset-bottom) + 6px)',
        backgroundColor: theme.palette.background.paper,
        borderTop: `1px solid ${theme.palette.divider}`,
        fontFamily: theme.typography.fontFamily,
      },
      tab: {
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 3,
        minWidth: 0,
        borderRadius: 12,
        fontSize: 11,
        fontWeight: 600,
        lineHeight: 1.3,
        color: theme.palette.text.secondary,
        textDecoration: 'none',
        WebkitTapHighlightColor: 'transparent',
        '&:focus-visible': {
          outline: `2px solid ${theme.palette.primary.main}`,
          outlineOffset: -2,
        },
      },
      button: {
        border: 0,
        padding: 0,
        background: 'none',
        fontFamily: 'inherit',
        cursor: 'pointer',
      },
      pill: {
        display: 'grid',
        placeItems: 'center',
        width: 56,
        height: 30,
        borderRadius: 999,
        '& svg': { fontSize: 22 },
      },
      current: {
        color: theme.palette.text.primary,
        '& $pill': {
          backgroundColor: vu.accentSoft,
          color: theme.palette.primary.main,
        },
      },
      // Offline, a section that needs the server
      unavailable: {
        color: theme.palette.text.disabled,
      },
      srOnly: {
        position: 'absolute',
        width: 1,
        height: 1,
        overflow: 'hidden',
        clip: 'rect(0 0 0 0)',
      },
    }
  },
  { name: 'VUTabBar' },
)

// Library opens albums, in the Listener's default album list (Personal >
// Default View). A default view that is another page (Artists, Playlists...)
// falls back to the app's default list, so Library always means albums.
const libraryPath = () => {
  const view = getStoredDefaultView()
  return `/album/${albumLists[view] ? view : defaultAlbumList}`
}

const TABS = [
  { id: 'library', icon: LibraryMusicIcon, to: libraryPath },
  { id: 'playlists', icon: QueueMusicIcon, to: () => '/playlist' },
  { id: 'stations', icon: HeadsetIcon, to: () => '/station' },
  {
    id: 'downloads',
    icon: GetAppIcon,
    to: () => '/downloads',
    shown: downloadsSupported,
  },
]

// The tab each page belongs to, by its first path segment; any other page
// is reached from More
const TAB_OF_SECTION = {
  '': 'library',
  album: 'library',
  artist: 'library',
  song: 'library',
  playlist: 'playlists',
  station: 'stations',
  downloads: 'downloads',
}

const tabOf = (pathname) => TAB_OF_SECTION[sectionOf(pathname)] || 'more'

const OFFLINE_HINT = 'vu-tab-bar-offline'

// TabBar is the main navigation on phones, in place of react-admin's side
// drawer: Library, Playlists, Stations, Downloads and More, along the bottom.
export const TabBar = () => {
  const classes = useStyles()
  const translate = useTranslate()
  const { pathname } = useLocation()
  const offline = useIsOffline()
  const [moreOpen, setMoreOpen] = useState(false)
  // The page's tab stays current (aria-current) while the sheet is open;
  // More only looks selected, its state given by aria-expanded
  const current = tabOf(pathname)
  const highlighted = moreOpen ? 'more' : current

  const label = (id) => translate(`menu.tabBar.${id}`)
  const content = (Icon, id) => (
    <>
      <span className={classes.pill}>
        <Icon aria-hidden="true" />
      </span>
      {label(id)}
    </>
  )

  return (
    <nav className={classes.root} aria-label={translate('menu.tabBar.label')}>
      {TABS.filter((t) => !t.shown || t.shown()).map(({ id, icon, to }) => {
        const path = to()
        // Still a link offline: the page explains why it can't open
        const unavailable = offline && !isAvailableOffline(path)
        return (
          <Link
            key={id}
            to={path}
            className={clsx(classes.tab, {
              [classes.current]: highlighted === id,
              [classes.unavailable]: unavailable,
            })}
            aria-current={current === id ? 'page' : undefined}
            aria-describedby={unavailable ? OFFLINE_HINT : undefined}
          >
            {content(icon, id)}
          </Link>
        )
      })}
      <button
        type="button"
        className={clsx(classes.tab, classes.button, {
          [classes.current]: highlighted === 'more',
        })}
        aria-current={current === 'more' ? 'true' : undefined}
        aria-haspopup="dialog"
        aria-expanded={moreOpen}
        onClick={() => setMoreOpen(true)}
      >
        {content(MoreHorizIcon, 'more')}
      </button>
      <span id={OFFLINE_HINT} className={classes.srOnly}>
        {translate('download.offline.unavailableTitle')}
      </span>
      <MoreSheet open={moreOpen} onClose={() => setMoreOpen(false)} />
    </nav>
  )
}

export default TabBar
