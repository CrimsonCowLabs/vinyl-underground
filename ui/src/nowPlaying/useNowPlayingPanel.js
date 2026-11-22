import { useSelector } from 'react-redux'
import { useMediaQuery } from '@material-ui/core'
import { isNowPlayingOpen, PANEL_QUERY } from './nowPlayingState'

// useWideEnoughForPanel is whether Now Playing is a side panel here, rather
// than full screen. It is right from the first render, so a panel restored on
// reload never opens full screen first.
export const useWideEnoughForPanel = () =>
  useMediaQuery(PANEL_QUERY, { noSsr: true })

// useNowPlayingPanelShown is whether the Now Playing side panel is showing.
export const useNowPlayingPanelShown = () => {
  const open = useSelector(isNowPlayingOpen)
  const wide = useWideEnoughForPanel()
  return open && wide
}
