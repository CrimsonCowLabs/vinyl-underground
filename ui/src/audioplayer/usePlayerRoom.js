import { useSelector } from 'react-redux'
import { useMediaQuery } from '@material-ui/core'
import { MINI_PLAYER_ROOM } from './MiniPlayer'

// usePlayerRoom is how much room page content leaves at its bottom, so the
// player never covers the end of a list or grid: the package's player bar on
// desktop (from 810px, as in Player), or a phone's MiniPlayer and the gap
// around it, above the home indicator. None while nothing is queued.
export const usePlayerRoom = () => {
  const queued = useSelector((state) => state.player?.queue.length > 0)
  const isDesktop = useMediaQuery('(min-width:810px)')
  if (!queued) return 0
  return isDesktop ? '80px' : MINI_PLAYER_ROOM
}
