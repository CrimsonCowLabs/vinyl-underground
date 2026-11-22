import React, { useEffect, useState } from 'react'
import { useTranslate } from 'react-admin'
import {
  Button,
  ButtonBase,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Drawer,
  Slider,
  Switch,
  TextField,
  Typography,
} from '@material-ui/core'
import { fade, makeStyles } from '@material-ui/core/styles'
import DeleteIcon from '@material-ui/icons/DeleteOutline'
import EditIcon from '@material-ui/icons/Edit'
import SaveIcon from '@material-ui/icons/SaveAlt'
import { vuPalette } from '../nowPlaying/vuPalette'
import { PANEL_QUERY, PANEL_WIDTH } from '../nowPlaying/nowPlayingState'
import { BANDS, peakDb } from './equalizerCurve'
import {
  BUILT_IN_PRESETS,
  MAX_DB,
  choosePreset,
  deletePreset,
  renamePreset,
  savePreset,
  setBand,
  setPreamp,
  turnOn,
} from './equalizerSettings'

const useStyles = makeStyles(
  (theme) => {
    const vu = vuPalette(theme)
    // Plain enough to see on the light sheet as well as the dark
    const railColour = fade(theme.palette.text.primary, 0.16)
    return {
      paper: {
        borderRadius: '12px 12px 0 0',
        paddingBottom: `calc(env(safe-area-inset-bottom) + ${theme.spacing(1.5)}px)`,
        maxWidth: 480,
        width: '100%',
        margin: '0 auto',
        maxHeight: '92vh',
        // Wide enough for the side panel: the sheet rises in the panel,
        // under the toolbar that opened it
        [`@media ${PANEL_QUERY}`]: {
          maxWidth: PANEL_WIDTH,
          marginRight: 0,
        },
      },
      head: {
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        padding: theme.spacing(1.5, 1.5, 0.5, 2.5),
      },
      title: { fontWeight: 700 },
      chips: {
        display: 'flex',
        gap: 8,
        overflowX: 'auto',
        padding: theme.spacing(0.5, 2.5, 1.5),
        scrollbarWidth: 'none',
        scrollPaddingInline: `${theme.spacing(2.5)}px`,
        '&::-webkit-scrollbar': { display: 'none' },
        // A scrolling row drops its end padding: this keeps the last preset
        // off the edge
        '&::after': { content: '""', flex: 'none', width: 12 },
      },
      chip: {
        flex: 'none',
        padding: '8px 14px',
        borderRadius: 999,
        fontSize: 13,
        fontWeight: 600,
        backgroundColor: vu.surface2,
        color: theme.palette.text.secondary,
      },
      chipActive: {
        backgroundColor: theme.palette.primary.main,
        color: theme.palette.primary.contrastText,
      },
      bands: {
        padding: theme.spacing(0, 2.5),
        transition: 'opacity 150ms',
      },
      off: { opacity: 0.5 },
      row: {
        display: 'grid',
        gridTemplateColumns: '56px 1fr 40px',
        alignItems: 'center',
        columnGap: 12,
        fontSize: 12,
        color: theme.palette.text.secondary,
        fontVariantNumeric: 'tabular-nums',
      },
      preampRow: {
        borderBottom: `1px solid ${theme.palette.divider}`,
        marginBottom: 4,
      },
      value: { textAlign: 'right', color: theme.palette.text.primary },
      slider: {
        // Comfortable to drag on a touch screen, without making rows tall
        padding: '15px 0',
        '@media (pointer: coarse)': { padding: '17px 0' },
      },
      // The rail is filled in the accent from 0 dB to the band's gain, so
      // the rows read as the curve
      rail: {
        height: 4,
        borderRadius: 2,
        opacity: 1,
        background: `linear-gradient(90deg, ${railColour} var(--lo), ${theme.palette.primary.main} var(--lo), ${theme.palette.primary.main} var(--hi), ${railColour} var(--hi))`,
      },
      thumb: { marginTop: -4 },
      auto: {
        gridColumn: '2 / 4',
        marginTop: -10,
        paddingBottom: 6,
        fontSize: 11,
      },
      actions: {
        display: 'flex',
        gap: 4,
        padding: theme.spacing(1, 1.5, 0),
      },
      spacer: { flex: 1 },
      unavailable: {
        padding: theme.spacing(1, 2.5, 2),
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'flex-start',
        gap: theme.spacing(1.5),
      },
    }
  },
  { name: 'NDEqualizerSheet' },
)

const bandLabel = (frequency) =>
  frequency >= 1000 ? `${frequency / 1000} kHz` : `${frequency} Hz`

// +6, 0, −3 (a real minus sign)
const formatDb = (db) => (db > 0 ? `+${db}` : db < 0 ? `−${Math.abs(db)}` : '0')

const GainRow = ({ label, value, onChange, className, children }) => {
  const classes = useStyles()
  const at = ((value + MAX_DB) / (2 * MAX_DB)) * 100
  return (
    <div className={`${classes.row} ${className || ''}`}>
      <span>{label}</span>
      <Slider
        className={classes.slider}
        classes={{ rail: classes.rail, thumb: classes.thumb }}
        style={{
          '--lo': `${Math.min(50, at)}%`,
          '--hi': `${Math.max(50, at)}%`,
        }}
        color="primary"
        min={-MAX_DB}
        max={MAX_DB}
        step={1}
        track={false}
        value={value}
        aria-label={label}
        getAriaValueText={(v) => `${formatDb(v)} dB`}
        onChange={(_, v) => v !== value && onChange(v)}
      />
      <span className={classes.value}>{formatDb(value)}</span>
      {children}
    </div>
  )
}

// PresetNameDialog asks for a custom preset's name, to save or rename it.
const PresetNameDialog = ({ title, initialName, onSave, onClose }) => {
  const translate = useTranslate()
  const [name, setName] = useState(initialName)
  useEffect(() => setName(initialName), [initialName])
  const submit = (event) => {
    event.preventDefault()
    if (!name.trim()) return
    onSave(name)
    onClose()
  }
  return (
    <Dialog
      open
      onClose={onClose}
      aria-labelledby="eq-preset-name-title"
      fullWidth
      maxWidth="xs"
    >
      <form onSubmit={submit}>
        <DialogTitle id="eq-preset-name-title">{title}</DialogTitle>
        <DialogContent>
          <TextField
            autoFocus
            fullWidth
            label={translate('nowPlaying.eq.presetName')}
            value={name}
            onChange={(event) => setName(event.target.value)}
            inputProps={{ maxLength: 40 }}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={onClose}>{translate('ra.action.cancel')}</Button>
          <Button type="submit" color="primary" disabled={!name.trim()}>
            {translate('nowPlaying.eq.saveButton')}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  )
}

// EqualizerSheet is the equalizer, in a sheet from the bottom: the on/off
// switch, the presets, the preamp and the ten bands, and saving, renaming
// and deleting custom presets. Each change is handed to onChange as new
// settings. Where audio effects aren't available (an iPhone with the Audio
// effects switch off) it points to that switch instead.
const EqualizerSheet = ({
  open,
  onClose,
  eq,
  available,
  onChange,
  onOpenSettings,
}) => {
  const classes = useStyles()
  const translate = useTranslate()
  const [naming, setNaming] = useState(null) // 'save' | 'rename' | null
  const title = translate('nowPlaying.eq.title')
  const custom = eq.custom.find((p) => p.id === eq.preset)
  const lowered = peakDb(eq.bands)

  // Keeps the preset picked in view along the row of presets, such as a
  // custom one just saved at the far end
  const [chips, setChips] = useState(null)
  const checkedId = eq.on ? eq.preset : null
  useEffect(() => {
    const checked = chips?.querySelector('[aria-checked="true"]')
    checked?.scrollIntoView?.({ block: 'nearest', inline: 'nearest' })
  }, [chips, checkedId])

  const presets = [
    ...BUILT_IN_PRESETS.map(({ id }) => ({
      id,
      name: translate(`nowPlaying.eq.presets.${id}`),
    })),
    ...eq.custom,
  ]

  return (
    <Drawer
      anchor="bottom"
      open={open}
      onClose={onClose}
      PaperProps={{ className: classes.paper, 'aria-label': title }}
    >
      <div className={classes.head}>
        <Typography component="h2" className={classes.title}>
          {title}
        </Typography>
        {available && (
          <Switch
            color="primary"
            checked={eq.on}
            onChange={(event) => onChange(turnOn(eq, event.target.checked))}
            inputProps={{ 'aria-label': translate('nowPlaying.eq.on') }}
          />
        )}
      </div>

      {!available ? (
        <div className={classes.unavailable}>
          <Typography variant="body2" color="textSecondary">
            {translate('nowPlaying.eq.needsAudioEffects')}
          </Typography>
          <Button variant="outlined" color="primary" onClick={onOpenSettings}>
            {translate('nowPlaying.eq.openSettings')}
          </Button>
        </div>
      ) : (
        <>
          <div
            ref={setChips}
            className={classes.chips}
            role="radiogroup"
            aria-label={translate('nowPlaying.eq.presetsLabel')}
          >
            {presets.map(({ id, name }) => {
              const checked = checkedId === id
              return (
                <ButtonBase
                  key={id}
                  role="radio"
                  aria-checked={checked}
                  className={
                    checked
                      ? `${classes.chip} ${classes.chipActive}`
                      : classes.chip
                  }
                  onClick={() => onChange(choosePreset(eq, id))}
                >
                  {name}
                </ButtonBase>
              )
            })}
          </div>

          <div className={`${classes.bands} ${eq.on ? '' : classes.off}`}>
            <GainRow
              label={translate('nowPlaying.eq.preamp')}
              value={eq.preamp}
              onChange={(db) => onChange(setPreamp(eq, db))}
              className={classes.preampRow}
            >
              {lowered > 0 && (
                <span className={classes.auto}>
                  {translate('nowPlaying.eq.autoPreamp', {
                    db: lowered.toFixed(1),
                  })}
                </span>
              )}
            </GainRow>
            {BANDS.map((frequency, index) => (
              <GainRow
                key={frequency}
                label={bandLabel(frequency)}
                value={eq.bands[index]}
                onChange={(db) => onChange(setBand(eq, index, db))}
              />
            ))}
          </div>

          <div className={classes.actions}>
            <Button
              size="small"
              startIcon={<SaveIcon />}
              onClick={() => setNaming('save')}
            >
              {translate('nowPlaying.eq.save')}
            </Button>
            {custom && (
              <>
                <span className={classes.spacer} />
                <Button
                  size="small"
                  startIcon={<EditIcon />}
                  onClick={() => setNaming('rename')}
                >
                  {translate('nowPlaying.eq.rename')}
                </Button>
                <Button
                  size="small"
                  startIcon={<DeleteIcon />}
                  onClick={() => onChange(deletePreset(eq, custom.id))}
                >
                  {translate('nowPlaying.eq.delete')}
                </Button>
              </>
            )}
          </div>
        </>
      )}

      {naming && (
        <PresetNameDialog
          title={translate(`nowPlaying.eq.${naming}Title`)}
          initialName={naming === 'rename' ? custom?.name || '' : ''}
          onSave={(name) =>
            onChange(
              naming === 'rename'
                ? renamePreset(eq, custom.id, name)
                : savePreset(eq, name),
            )
          }
          onClose={() => setNaming(null)}
        />
      )}
    </Drawer>
  )
}

export default EqualizerSheet
