import React from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { useTranslate } from 'react-admin'
import {
  FormControl,
  FormHelperText,
  FormLabel,
  Radio,
  RadioGroup,
  Tooltip,
} from '@material-ui/core'
import { makeStyles, useTheme } from '@material-ui/core/styles'
import CheckIcon from '@material-ui/icons/Check'
import { changeAccent } from '../actions'
import {
  currentAccentId,
  showsVinylUnderground,
} from '../reducers/themeReducer'
import { accents, ART_ACCENT } from '../themes/vinylUndergroundPalette'

const SWATCH = 32

const useStyles = makeStyles(
  (theme) => ({
    root: { marginBottom: theme.spacing(2) },
    label: { marginBottom: theme.spacing(1) },
    radio: { padding: 6 },
    swatch: {
      width: SWATCH,
      height: SWATCH,
      borderRadius: '50%',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      boxShadow: `inset 0 0 0 1px ${theme.palette.divider}`,
    },
    checked: {
      // A ring in the text colour, clear of the swatch
      boxShadow: `0 0 0 2px ${theme.palette.background.paper}, 0 0 0 4px ${theme.palette.text.primary}`,
    },
    art: {
      background:
        'conic-gradient(#FF5B2E, #FF9A1F, #E2B53A, #9DBB4E, #3EC1B0, #6FA3F0, #C08BF0, #FF6F91, #FF5B2E)',
    },
    check: { fontSize: 18 },
  }),
  { name: 'NDSelectAccent' },
)

const Swatch = ({ id, checked }) => {
  const classes = useStyles()
  const theme = useTheme()
  const isArt = id === ART_ACCENT
  const accent = isArt ? null : accents[id][theme.palette.type]
  return (
    <span
      className={`${classes.swatch} ${isArt ? classes.art : ''} ${checked ? classes.checked : ''}`}
      style={accent ? { backgroundColor: accent.main } : undefined}
    >
      {checked && (
        <CheckIcon
          className={classes.check}
          style={{ color: accent ? accent.contrastText : '#0F1216' }}
        />
      )}
    </span>
  )
}

// SelectAccent picks the Vinyl Underground theme's accent: one of its fixed
// accents, or "From album art". It's kept per device, and offered only while
// the Vinyl Underground theme is shown.
export const SelectAccent = () => {
  const classes = useStyles()
  const translate = useTranslate()
  const dispatch = useDispatch()
  const shown = useSelector(showsVinylUnderground)
  const current = useSelector(currentAccentId)
  if (!shown) return null

  const name = (id) => translate(`menu.personal.options.accents.${id}`)
  const ids = [...Object.keys(accents), ART_ACCENT]
  return (
    <FormControl component="fieldset" className={classes.root}>
      <FormLabel component="legend" className={classes.label}>
        {translate('menu.personal.options.accent')}: {name(current)}
      </FormLabel>
      <RadioGroup
        row
        aria-label={translate('menu.personal.options.accent')}
        value={current}
        onChange={(event) => dispatch(changeAccent(event.target.value))}
      >
        {ids.map((id) => (
          <Tooltip key={id} title={name(id)}>
            <Radio
              className={classes.radio}
              value={id}
              inputProps={{ 'aria-label': name(id) }}
              icon={<Swatch id={id} />}
              checkedIcon={<Swatch id={id} checked />}
            />
          </Tooltip>
        ))}
      </RadioGroup>
      {current === ART_ACCENT && (
        <FormHelperText>
          {translate('menu.personal.options.accentArtHelp')}
        </FormHelperText>
      )}
    </FormControl>
  )
}
