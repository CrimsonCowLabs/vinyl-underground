import React from 'react'
import RecordArt from './RecordArt'
import VisualArea from '../visuals/VisualArea'
import { useVisualShown } from '../visuals/visualsSettings'

// ArtTab is what Now Playing's Art tab shows: the cover on its record or,
// with a visual chosen in the Visuals tool (#96), the visual, when the tab is
// labelled Visuals. audioInstance is the player's audio element; the visual
// reads it through the shared audio chain. Being unmounted on another tab is
// what stops a visual drawing there.
const ArtTab = ({ cover, alt, playing, audioInstance }) => {
  const visual = useVisualShown()
  if (!visual) return <RecordArt cover={cover} alt={alt} playing={playing} />
  return (
    <VisualArea
      mode={visual}
      cover={cover}
      alt={alt}
      playing={playing}
      audioInstance={audioInstance}
    />
  )
}

export default ArtTab
