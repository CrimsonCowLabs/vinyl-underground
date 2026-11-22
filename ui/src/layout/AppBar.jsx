import React, { createElement, forwardRef, Fragment } from 'react'
import {
  AppBar as RAAppBar,
  MenuItemLink,
  useTranslate,
  usePermissions,
  getResources,
} from 'react-admin'
import { MdInfo, MdPhonelink } from 'react-icons/md'
import { useSelector } from 'react-redux'
import {
  makeStyles,
  MenuItem,
  ListItemIcon,
  Divider,
  useMediaQuery,
} from '@material-ui/core'
import ViewListIcon from '@material-ui/icons/ViewList'
import { Dialogs } from '../dialogs/Dialogs'
import { AboutDialog, QuickConnectDialog } from '../dialogs'
import PersonalMenu from './PersonalMenu'
import ActivityPanel from './ActivityPanel'
import WhosListeningPanel from './WhosListeningPanel'
import UserMenu from './UserMenu'
import config from '../config'
import { OfflineIndicator } from '../download/OfflineIndicator'
import { settingsResources, userSettingsLink } from './navItems'

const useStyles = makeStyles(
  (theme) => ({
    root: {
      color: theme.palette.text.secondary,
    },
    active: {
      color: theme.palette.text.primary,
    },
    icon: { minWidth: theme.spacing(5) },
  }),
  {
    name: 'NDAppBar',
  },
)

const DialogMenuItem = forwardRef(
  ({ onClick, label, icon, dialog, ...rest }, ref) => {
    const classes = useStyles(rest)
    const [open, setOpen] = React.useState(false)

    const handleClose = () => {
      onClick && onClick()
      setOpen(false)
    }
    return (
      <>
        <MenuItem
          ref={ref}
          onClick={() => setOpen(true)}
          className={classes.root}
        >
          <ListItemIcon className={classes.icon}>
            {createElement(icon, { title: label, size: 24 })}
          </ListItemIcon>
          {label}
        </MenuItem>
        {createElement(dialog, { onClose: handleClose, open })}
      </>
    )
  },
)

DialogMenuItem.displayName = 'DialogMenuItem'

const CustomUserMenu = ({ onClick, ...rest }) => {
  const translate = useTranslate()
  const resources = useSelector(getResources)
  const classes = useStyles(rest)
  const { permissions } = usePermissions()

  const renderUserMenuItemLink = () => {
    const link = userSettingsLink(resources, permissions)
    return link && renderSettingsMenuItemLink({ name: 'user', ...link })
  }

  const renderSettingsMenuItemLink = ({
    name,
    icon,
    to = `/${name}`,
    smartCount = 2,
  }) => {
    const label = translate(`resources.${name}.name`, {
      smart_count: smartCount,
    })
    return (
      <MenuItemLink
        className={classes.root}
        activeClassName={classes.active}
        key={name}
        to={to}
        primaryText={label}
        leftIcon={
          (icon && createElement(icon, { size: 24 })) || <ViewListIcon />
        }
        onClick={onClick}
        sidebarIsOpen={true}
      />
    )
  }

  return (
    <>
      <OfflineIndicator />
      {config.devActivityPanel &&
        permissions === 'admin' &&
        config.enableNowPlaying && <WhosListeningPanel />}
      {config.devActivityPanel && permissions === 'admin' && <ActivityPanel />}
      <UserMenu {...rest}>
        <PersonalMenu sidebarIsOpen={true} onClick={onClick} />
        {config.enableQuickConnect && (
          <DialogMenuItem
            label={translate('menu.quickConnect.name')}
            icon={MdPhonelink}
            dialog={QuickConnectDialog}
          />
        )}
        <Divider />
        {renderUserMenuItemLink()}
        {settingsResources(resources).map(renderSettingsMenuItemLink)}
        <Divider />
        <DialogMenuItem
          label={translate('menu.about')}
          icon={MdInfo}
          dialog={AboutDialog}
        />
      </UserMenu>
      <Dialogs />
    </>
  )
}

const usePhoneStyles = makeStyles((theme) => ({
  noMenuButton: { display: 'none' },
  // Off the edge, where the button was
  title: { paddingLeft: theme.spacing(2) },
}))

// On a phone the tab bar replaces the side drawer (Layout), so the button
// that opens the drawer goes too
const AppBar = (props) => {
  const isDesktop = useMediaQuery('(min-width:810px)')
  const classes = usePhoneStyles()
  return (
    <RAAppBar
      {...props}
      classes={
        isDesktop
          ? undefined
          : { menuButton: classes.noMenuButton, title: classes.title }
      }
      container={Fragment}
      userMenu={<CustomUserMenu />}
    />
  )
}

export default AppBar
