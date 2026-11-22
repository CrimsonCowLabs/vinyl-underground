// The Lyrics and Queue tools in the Now Playing toolbar (#92), registered at
// load like the other tools. Each switches the art area to its tab, or back
// to Art when its tab is showing already.
import React from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { useTranslate } from 'react-admin'
import SubjectIcon from '@material-ui/icons/Subject'
import QueueMusicIcon from '@material-ui/icons/QueueMusic'
import { registerNowPlayingTool } from './toolbarSlots'
import { NowPlayingToolButton } from './NowPlayingToolbar'
import { nowPlayingTab, toggleNowPlayingTab } from './nowPlayingState'

const tabTool = (tab, icon) => {
  const TabTool = () => {
    const translate = useTranslate()
    const dispatch = useDispatch()
    const selected = useSelector(nowPlayingTab) === tab
    return (
      <NowPlayingToolButton
        icon={icon}
        label={translate(`nowPlaying.tabs.${tab}`)}
        active={selected}
        onClick={() => dispatch(toggleNowPlayingTab(tab))}
      />
    )
  }
  TabTool.displayName = `TabTool(${tab})`
  return TabTool
}

registerNowPlayingTool('lyrics', tabTool('lyrics', <SubjectIcon />))
registerNowPlayingTool('queue', tabTool('queue', <QueueMusicIcon />))
