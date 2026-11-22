import React from 'react'
import { useTranslate } from 'react-admin'
import { ButtonBase } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import { registeredTools } from './toolbarSlots'
import { vuPalette } from './vuPalette'

const useStyles = makeStyles(
  (theme) => {
    const vu = vuPalette(theme)
    return {
      root: {
        display: 'grid',
        gridAutoFlow: 'column',
        gridAutoColumns: 'minmax(0, 1fr)',
        gap: 2,
        paddingTop: 4,
        borderTop: `1px solid ${theme.palette.divider}`,
      },
      button: {
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        gap: 3,
        padding: '6px 0',
        borderRadius: 12,
        fontSize: 11,
        fontWeight: 600,
        color: theme.palette.text.secondary,
        '& svg': { width: 22, height: 22 },
      },
      active: {
        color: theme.palette.primary.main,
        backgroundColor: vu.accentSoft,
      },
    }
  },
  { name: 'NDNowPlayingToolbar' },
)

// NowPlayingToolButton is how a tool looks in the toolbar: an icon over a
// short label, or over its current value (a sleep timer's time left, a
// speed). active marks a tool that is on or open.
export const NowPlayingToolButton = ({
  icon,
  label,
  value,
  active = false,
  onClick,
  ...rest
}) => {
  const classes = useStyles()
  return (
    <ButtonBase
      className={
        active ? `${classes.button} ${classes.active}` : classes.button
      }
      aria-label={value ? `${label}: ${value}` : label}
      aria-pressed={active}
      onClick={onClick}
      {...rest}
    >
      {icon}
      <span>{value || label}</span>
    </ButtonBase>
  )
}

// NowPlayingToolbar shows the tools registered for its slots, in slot order,
// and nothing at all while there are none.
const NowPlayingToolbar = ({ onNavigate }) => {
  const classes = useStyles()
  const translate = useTranslate()
  const tools = registeredTools()
  if (!tools.length) return null
  return (
    <div
      className={classes.root}
      role="toolbar"
      aria-label={translate('nowPlaying.tools')}
    >
      {tools.map(([slot, Tool]) => (
        <Tool key={slot} onNavigate={onNavigate} />
      ))}
    </div>
  )
}

export default NowPlayingToolbar
