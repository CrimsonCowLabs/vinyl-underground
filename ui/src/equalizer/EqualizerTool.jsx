import React, { useState } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { useTranslate } from 'react-admin'
import EqualizerIcon from '@material-ui/icons/Equalizer'
import { NowPlayingToolButton } from '../nowPlaying/NowPlayingToolbar'
import { useAudioEffectsAvailable } from '../audioplayer/audioEffects'
import { setEqualizer } from '../actions'
import { presetName, selectEqualizer } from './equalizerSettings'
import { waitsForTrack } from './equalizer'
import { audioChain } from '../audioplayer/audioChain'
import EqualizerSheet from './EqualizerSheet'

// EqualizerTool is the toolbar's EQ: it shows the preset while the
// equalizer is on, and opens the equalizer's sheet. The player applies it
// (useEqualizer).
const EqualizerTool = ({ onNavigate }) => {
  const translate = useTranslate()
  const dispatch = useDispatch()
  const eq = useSelector(selectEqualizer)
  const available = useAudioEffectsAvailable()
  const isRadio = useSelector((state) => !!state.player?.current?.isRadio)
  const [open, setOpen] = useState(false)
  const on = available && eq.on

  return (
    <>
      <NowPlayingToolButton
        icon={<EqualizerIcon />}
        label={translate('nowPlaying.eq.label')}
        value={on ? presetName(eq, translate) : undefined}
        active={on}
        aria-haspopup="true"
        onClick={() => setOpen(true)}
      />
      <EqualizerSheet
        open={open}
        onClose={() => setOpen(false)}
        eq={eq}
        available={available}
        radioWaits={waitsForTrack(eq, {
          available,
          isRadio,
          built: audioChain.isBuilt(),
        })}
        onChange={(next) => dispatch(setEqualizer(next))}
        onOpenSettings={() => {
          setOpen(false)
          onNavigate('/personal')
        }}
      />
    </>
  )
}

export default EqualizerTool
