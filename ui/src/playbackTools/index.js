// The Sleep timer and Speed tools in the Now Playing toolbar (#93),
// registered at load so the toolbar has them from its first render. The
// player wires them to its audio element: sleepTimer.attach and
// usePlaybackSpeed.
import { registerNowPlayingTool } from '../nowPlaying/toolbarSlots'
import SleepTool from './SleepTool'
import SpeedTool from './SpeedTool'

registerNowPlayingTool('sleep', SleepTool)
registerNowPlayingTool('speed', SpeedTool)

export { sleepTimer } from './sleepTimer'
export { usePlaybackSpeed } from './playbackSpeed'
