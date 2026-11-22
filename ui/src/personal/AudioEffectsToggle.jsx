import { useTranslate } from 'react-admin'
import { useDispatch, useSelector } from 'react-redux'
import {
  FormControl,
  FormControlLabel,
  FormHelperText,
  Switch,
} from '@material-ui/core'
import { setAudioEffects } from '../actions'

// The iPhone's Audio effects switch (ADR-0010). Personal shows it on iOS only.
export const AudioEffectsToggle = () => {
  const translate = useTranslate()
  const dispatch = useDispatch()
  const enabled = useSelector((state) => state.settings.audioEffects === true)

  return (
    <FormControl>
      <FormControlLabel
        control={
          <Switch
            id="audioEffects"
            color="primary"
            checked={enabled}
            onChange={(event) =>
              dispatch(setAudioEffects(event.target.checked))
            }
            inputProps={{ 'aria-describedby': 'audio-effects-helper-text' }}
          />
        }
        label={
          <span>{translate('menu.personal.options.audioEffects.name')}</span>
        }
      />
      <FormHelperText id="audio-effects-helper-text">
        {translate('menu.personal.options.audioEffects.help')}
      </FormHelperText>
    </FormControl>
  )
}
