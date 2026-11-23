import React from 'react'
import RecordArt from './RecordArt'

// ArtTab is what Now Playing's Art tab shows: the cover on its record. It is
// the seam for visuals (#96): with a visual chosen, this tab becomes
// Art/Visuals and shows the visual here in place of RecordArt, with the same
// props, audioInstance (the player's audio element) among them. Nothing else in Now
// Playing needs to change.
const ArtTab = ({ cover, alt, playing }) => (
  <RecordArt cover={cover} alt={alt} playing={playing} />
)

export default ArtTab
