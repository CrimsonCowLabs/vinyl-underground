import { openNowPlaying } from '../nowPlaying/nowPlayingState'

// nowPlayingPlayerProps are the player package's props that hand its
// full-screen view over to Now Playing. On a phone the package stays in its
// mini mode and never opens its own mobile panel (toggleMode is off); tapping
// the mini player reaches onCoverClick, which opens Now Playing instead. On
// desktop it keeps its full player bar, whose cover goes to the album (Now
// Playing's side panel there is #91).
export const nowPlayingPlayerProps = (isDesktop, dispatch) => ({
  mode: isDesktop ? 'full' : 'mini',
  toggleMode: false,
  onCoverClick: (mode, audioLists, audioInfo) => {
    if (mode === 'mini') {
      dispatch(openNowPlaying())
    } else if (audioInfo?.song?.albumId) {
      window.location.href = `#/album/${audioInfo.song.albumId}/show`
    }
  },
})
