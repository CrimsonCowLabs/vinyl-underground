import React, { createElement, isValidElement, useState } from 'react'
import PropTypes from 'prop-types'
import { useSelector } from 'react-redux'
import { Link } from 'react-router-dom'
import { getResources, usePermissions, useTranslate } from 'react-admin'
import { Drawer, Typography } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import clsx from 'clsx'
import { MdTune } from 'react-icons/md'
import ViewListIcon from '@material-ui/icons/ViewList'
import AutorenewIcon from '@material-ui/icons/Autorenew'
import albumLists from '../album/albumLists'
import LibrarySelector from '../common/LibrarySelector'
import config from '../config'
import { isAvailableOffline, useIsOffline } from '../download'
import { GeneratePlaylistDialog } from '../playlist/GeneratePlaylistButton'
import { vuPalette } from '../nowPlaying/vuPalette'
import {
  settingsResources,
  subMenuResources,
  translatedResourceName,
  userSettingsLink,
} from './navItems'

const useStyles = makeStyles(
  (theme) => {
    const vu = vuPalette(theme)
    return {
      paper: {
        maxHeight: '85vh',
        borderRadius: '12px 12px 0 0',
        backgroundColor: theme.palette.background.paper,
        padding: '8px 16px',
        paddingBottom: 'calc(env(safe-area-inset-bottom) + 16px)',
      },
      handle: {
        width: 36,
        height: 4,
        borderRadius: 2,
        margin: '0 auto 8px',
        backgroundColor: theme.palette.divider,
      },
      title: { fontWeight: 700, margin: '4px 0 8px' },
      section: {
        margin: '16px 0 8px',
        fontSize: 12,
        fontWeight: 700,
        letterSpacing: '0.08em',
        textTransform: 'uppercase',
        color: theme.palette.text.secondary,
      },
      grid: {
        display: 'grid',
        gridTemplateColumns: 'repeat(2, minmax(0, 1fr))',
        gap: 8,
        margin: 0,
        padding: 0,
        listStyle: 'none',
      },
      item: {
        display: 'flex',
        alignItems: 'center',
        gap: 10,
        width: '100%',
        boxSizing: 'border-box',
        minHeight: 48,
        padding: '8px 12px',
        border: 0,
        borderRadius: 12,
        backgroundColor: vu.surface2,
        color: theme.palette.text.primary,
        font: 'inherit',
        fontSize: 14,
        fontWeight: 600,
        textAlign: 'left',
        textDecoration: 'none',
        cursor: 'pointer',
        '& svg': {
          flex: 'none',
          fontSize: 22,
          color: theme.palette.text.secondary,
        },
        '&:focus-visible': {
          outline: `2px solid ${theme.palette.primary.main}`,
          outlineOffset: 2,
        },
      },
      label: {
        minWidth: 0,
        overflow: 'hidden',
        textOverflow: 'ellipsis',
      },
      // Offline, a page that needs the server
      disabled: {
        cursor: 'default',
        color: theme.palette.text.disabled,
        '& svg': { color: theme.palette.text.disabled },
      },
    }
  },
  { name: 'VUMoreSheet' },
)

// Resource icons are elements (the drawer's) or components (the user menu's)
const iconOf = (icon) =>
  !icon ? (
    <ViewListIcon />
  ) : isValidElement(icon) ? (
    icon
  ) : (
    createElement(icon, { size: 22 })
  )

const TITLE_ID = 'vu-more-sheet-title'

// MoreSheet is the phone's "More" tab: everything the side drawer and the
// user menu offer that no tab opens. What it holds follows the resources
// App.jsx registers (by role and config), as the drawer does, so an item the
// drawer hides stays hidden here too.
export const MoreSheet = ({ open, onClose }) => {
  const classes = useStyles()
  const translate = useTranslate()
  const resources = useSelector(getResources)
  const { permissions } = usePermissions()
  const offline = useIsOffline()
  const [generating, setGenerating] = useState(false)

  const renderLink = ({ key, to, label, icon }) => {
    const content = (
      <>
        {iconOf(icon)}
        <span className={classes.label}>{label}</span>
      </>
    )
    return (
      <li key={key}>
        {offline && !isAvailableOffline(to) ? (
          <div
            className={clsx(classes.item, classes.disabled)}
            aria-disabled="true"
          >
            {content}
          </div>
        ) : (
          <Link to={to} className={classes.item} onClick={onClose}>
            {content}
          </Link>
        )}
      </li>
    )
  }

  const section = (title, items) =>
    items.length > 0 && (
      <>
        <h3 className={classes.section}>{title}</h3>
        <ul className={classes.grid}>{items}</ul>
      </>
    )

  const albumItems = resources.some((r) => r.name === 'album')
    ? Object.keys(albumLists).map((type) =>
        renderLink({
          key: type,
          to: `/album/${type}`,
          label: translate(`resources.album.lists.${type}`),
          icon: albumLists[type].icon,
        }),
      )
    : []

  // Stations have their own tab
  const libraryItems = subMenuResources(resources, undefined)
    .filter((r) => r.name !== 'station')
    .map((r) =>
      renderLink({
        key: r.name,
        to: `/${r.name}`,
        label: translatedResourceName(r, translate),
        icon: r.icon,
      }),
    )

  const playlistItems = config.enableGeneratedPlaylists
    ? [
        <li key="generate">
          <button
            type="button"
            className={clsx(classes.item, { [classes.disabled]: offline })}
            disabled={offline}
            onClick={() => {
              setGenerating(true)
              onClose()
            }}
          >
            <AutorenewIcon />
            <span className={classes.label}>
              {translate('menu.more.generatePlaylist')}
            </span>
          </button>
        </li>,
      ]
    : []

  const userLink = userSettingsLink(resources, permissions)
  const settingsItems = [
    renderLink({
      key: 'personal',
      to: '/personal',
      label: translate('menu.personal.name'),
      icon: MdTune,
    }),
    userLink &&
      renderLink({
        key: 'user',
        label: translate('resources.user.name', {
          smart_count: userLink.smartCount,
        }),
        ...userLink,
      }),
    ...settingsResources(resources).map((r) =>
      renderLink({
        key: r.name,
        to: `/${r.name}`,
        label: translatedResourceName(r, translate),
        icon: r.icon,
      }),
    ),
  ].filter(Boolean)

  return (
    <>
      <Drawer
        anchor="bottom"
        open={open}
        onClose={onClose}
        classes={{ paper: classes.paper }}
        PaperProps={{
          role: 'dialog',
          'aria-modal': true,
          'aria-labelledby': TITLE_ID,
        }}
      >
        <div className={classes.handle} aria-hidden="true" />
        <Typography
          id={TITLE_ID}
          variant="h6"
          component="h2"
          className={classes.title}
        >
          {translate('menu.tabBar.more')}
        </Typography>
        <LibrarySelector />
        {section(translate('menu.albumList'), albumItems)}
        {section(translate('menu.library'), libraryItems)}
        {section(translate('menu.playlists'), playlistItems)}
        {section(translate('menu.settings'), settingsItems)}
      </Drawer>
      {generating && (
        <GeneratePlaylistDialog open onClose={() => setGenerating(false)} />
      )}
    </>
  )
}

MoreSheet.propTypes = {
  open: PropTypes.bool.isRequired,
  onClose: PropTypes.func.isRequired,
}

export default MoreSheet
