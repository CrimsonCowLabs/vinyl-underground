// Visuals (#96): the Visuals tool in the Now Playing toolbar, registered at
// load so the toolbar has it from its first render. The visual itself plays
// in Now Playing's Art tab (nowPlaying/ArtTab).
import { registerNowPlayingTool } from '../nowPlaying/toolbarSlots'
import VisualsTool from './VisualsTool'

registerNowPlayingTool('visuals', VisualsTool)
