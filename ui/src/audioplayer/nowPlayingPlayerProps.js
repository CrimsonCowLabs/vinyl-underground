import { openNowPlaying, toggleNowPlaying } from '../nowPlaying/nowPlayingState'

// nowPlayingPlayerProps are the player package's props that hand its
// full-screen view over to Now Playing. On a phone the package stays in its
// mini mode and never opens its own mobile panel (toggleMode is off); tapping
// the mini player reaches onCoverClick, which opens Now Playing instead. On
// desktop it keeps its full player bar, whose cover opens and closes Now
// Playing (the side panel on a wide window, full screen on a narrower one).
export const nowPlayingPlayerProps = (isDesktop, dispatch) => ({
  mode: isDesktop ? 'full' : 'mini',
  toggleMode: false,
  onCoverClick: (mode) =>
    dispatch(mode === 'mini' ? openNowPlaying() : toggleNowPlaying()),
})
