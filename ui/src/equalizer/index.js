// The equalizer (#95): the EQ tool in the Now Playing toolbar, registered at
// load so the toolbar has it from its first render, and useEqualizer, which
// the player calls to keep it on its audio.
import { registerNowPlayingTool } from '../nowPlaying/toolbarSlots'
import EqualizerTool from './EqualizerTool'

registerNowPlayingTool('eq', EqualizerTool)

export { useEqualizer } from './equalizer'
