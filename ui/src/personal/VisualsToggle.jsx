import { useTranslate } from 'react-admin'
import { useDispatch } from 'react-redux'
import {
  FormControl,
  FormControlLabel,
  FormHelperText,
  Switch,
} from '@material-ui/core'
import { setVisuals } from '../actions'
import { useVisuals } from '../visuals/visualsSettings'

// The switch that turns Now Playing's visuals off entirely (#96): the
// Visuals tool goes, and the art area shows the album art. The visual chosen
// is kept for if they are turned on again.
export const VisualsToggle = () => {
  const translate = useTranslate()
  const dispatch = useDispatch()
  const { enabled } = useVisuals()

  return (
    <FormControl>
      <FormControlLabel
        control={
          <Switch
            id="visuals"
            color="primary"
            checked={enabled}
            onChange={(event) =>
              dispatch(setVisuals({ enabled: event.target.checked }))
            }
            inputProps={{ 'aria-describedby': 'visuals-helper-text' }}
          />
        }
        label={<span>{translate('menu.personal.options.visuals.name')}</span>}
      />
      <FormHelperText id="visuals-helper-text">
        {translate('menu.personal.options.visuals.help')}
      </FormHelperText>
    </FormControl>
  )
}
