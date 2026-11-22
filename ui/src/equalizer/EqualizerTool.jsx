import React, { useState } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { useTranslate } from 'react-admin'
import EqualizerIcon from '@material-ui/icons/Equalizer'
import { NowPlayingToolButton } from '../nowPlaying/NowPlayingToolbar'
import { useAudioEffectsAvailable } from '../audioplayer/audioEffects'
import { setEqualizer } from '../actions'
import { BUILT_IN_PRESETS, selectEqualizer } from './equalizerSettings'
import EqualizerSheet from './EqualizerSheet'

// presetName is what the curve is called: its preset's name, or "Custom"
// once the Listener has moved a slider.
const presetName = (eq, translate) => {
  if (BUILT_IN_PRESETS.some((p) => p.id === eq.preset)) {
    return translate(`nowPlaying.eq.presets.${eq.preset}`)
  }
  const custom = eq.custom.find((p) => p.id === eq.preset)
  return custom ? custom.name : translate('nowPlaying.eq.custom')
}

// EqualizerTool is the toolbar's EQ: it shows the preset while the
// equalizer is on, and opens the equalizer's sheet. The player applies it
// (useEqualizer).
const EqualizerTool = ({ onNavigate }) => {
  const translate = useTranslate()
  const dispatch = useDispatch()
  const eq = useSelector(selectEqualizer)
  const available = useAudioEffectsAvailable()
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
