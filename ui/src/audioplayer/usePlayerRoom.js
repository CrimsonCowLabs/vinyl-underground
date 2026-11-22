import { useSelector } from 'react-redux'
import { useMediaQuery } from '@material-ui/core'
import { MINI_PLAYER_ROOM } from './MiniPlayer'
import { aboveTabBar } from '../layout/tabBarRoom'

// usePlayerRoom is how much room page content leaves at its bottom, so
// nothing fixed there covers the end of a list or grid: the package's player
// bar on desktop (from 810px, as in Player), none there while nothing is
// queued; on a phone, the bottom tab bar above the home indicator and, while
// something is queued, the MiniPlayer above that.
export const usePlayerRoom = () => {
  const queued = useSelector((state) => state.player?.queue.length > 0)
  const isDesktop = useMediaQuery('(min-width:810px)')
  if (isDesktop) return queued ? '80px' : 0
  return aboveTabBar(queued ? MINI_PLAYER_ROOM : 0)
}
