import { humanize, pluralize } from 'inflection'
import { MdPerson, MdSupervisorAccount } from 'react-icons/md'
import config from '../config'

// What the navigation offers is decided where the resources are registered
// (App.jsx: by role and config), so the side drawer, the user menu and the
// phone's More sheet all read the registered resources through these.

// translatedResourceName is the resource's name in the plural, e.g. "Songs"
export const translatedResourceName = (resource, translate) =>
  translate(`resources.${resource.name}.name`, {
    smart_count: 2,
    _:
      resource.options && resource.options.label
        ? translate(resource.options.label, {
            smart_count: 2,
            _: resource.options.label,
          })
        : humanize(pluralize(resource.name)),
  })

// The resources listed in the drawer under a sub menu: 'playlist',
// 'settings', or undefined for those at its top level
export const subMenuResources = (resources, subMenu) =>
  resources.filter(
    (resource) =>
      resource.hasList &&
      resource.options &&
      resource.options.subMenu === subMenu,
  )

// The settings and admin pages in the user menu, after the Listener's own
// link
export const settingsResources = (resources) =>
  subMenuResources(resources, 'settings').filter((r) => r.name !== 'user')

// The Users link in the settings: all of them for an admin; otherwise the
// Listener's own profile, when they may edit it
export const userSettingsLink = (resources, permissions) => {
  if (!resources.some((r) => r?.name === 'user')) return null
  if (permissions === 'admin') {
    return { to: '/user', smartCount: 2, icon: MdSupervisorAccount }
  }
  if (!config.enableUserEditing) return null
  return {
    to: `/user/${localStorage.getItem('userId')}`,
    smartCount: 1,
    icon: MdPerson,
  }
}
