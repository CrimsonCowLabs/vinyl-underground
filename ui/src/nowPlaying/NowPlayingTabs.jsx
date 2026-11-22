import React, { useRef } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { useTranslate } from 'react-admin'
import { ButtonBase } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import {
  NOW_PLAYING_TABS,
  nowPlayingTab,
  panelId,
  showNowPlayingTab,
  tabId,
} from './nowPlayingState'
import { vuPalette } from './vuPalette'

const useStyles = makeStyles(
  (theme) => {
    const vu = vuPalette(theme)
    return {
      root: {
        alignSelf: 'center',
        display: 'flex',
        padding: 3,
        borderRadius: 999,
        backgroundColor: vu.surface2,
      },
      tab: {
        padding: '6px 16px',
        minHeight: 32,
        borderRadius: 999,
        fontSize: 13,
        fontWeight: 700,
        color: theme.palette.text.secondary,
        '&[aria-selected="true"]': {
          backgroundColor: vu.surface3,
          color: theme.palette.text.primary,
          boxShadow: '0 1px 3px rgba(0, 0, 0, 0.25)',
        },
        '&:focus-visible': {
          outline: `2px solid ${theme.palette.primary.main}`,
        },
      },
    }
  },
  { name: 'NDNowPlayingTabs' },
)

// NowPlayingTabs switches Now Playing's art area between Art, Lyrics and
// Queue. The choice is kept per device (see nowPlayingState). The arrow keys
// move between the tabs, as in any tab list.
const NowPlayingTabs = () => {
  const classes = useStyles()
  const translate = useTranslate()
  const dispatch = useDispatch()
  const selected = useSelector(nowPlayingTab)
  const refs = useRef({})

  const onKeyDown = (e) => {
    const step = { ArrowRight: 1, ArrowLeft: -1 }[e.key]
    if (!step) return
    e.preventDefault()
    const count = NOW_PLAYING_TABS.length
    const next =
      NOW_PLAYING_TABS[
        (NOW_PLAYING_TABS.indexOf(selected) + step + count) % count
      ]
    dispatch(showNowPlayingTab(next))
    refs.current[next]?.focus()
  }

  return (
    <div
      className={classes.root}
      role="tablist"
      aria-label={translate('nowPlaying.tabs.label')}
    >
      {NOW_PLAYING_TABS.map((tab) => (
        <ButtonBase
          key={tab}
          id={tabId(tab)}
          ref={(el) => (refs.current[tab] = el)}
          className={classes.tab}
          role="tab"
          aria-selected={tab === selected}
          aria-controls={panelId}
          tabIndex={tab === selected ? 0 : -1}
          onClick={() => dispatch(showNowPlayingTab(tab))}
          onKeyDown={onKeyDown}
        >
          {translate(`nowPlaying.tabs.${tab}`)}
        </ButtonBase>
      ))}
    </div>
  )
}

export default NowPlayingTabs
