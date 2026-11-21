import React from 'react'
import { MdCloudDownload } from 'react-icons/md'
import RipList from './RipList'

export default {
  list: RipList,
  // The sidebar's MenuItemLink clones this, so it must be an element, not a
  // component: a component here crashes the whole layout on login
  icon: <MdCloudDownload />,
}
