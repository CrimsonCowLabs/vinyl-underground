import React from 'react'
import {
  Drawer,
  ListItemIcon,
  ListItemText,
  MenuItem,
  MenuList,
  Typography,
} from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import CheckIcon from '@material-ui/icons/Check'

const useStyles = makeStyles(
  (theme) => ({
    paper: {
      borderRadius: '12px 12px 0 0',
      paddingBottom: `calc(env(safe-area-inset-bottom) + ${theme.spacing(1)}px)`,
      maxWidth: 480,
      margin: '0 auto',
    },
    title: {
      padding: theme.spacing(2, 2.5, 1),
      fontWeight: 700,
    },
    item: { minHeight: 48 },
    check: { minWidth: 36, color: theme.palette.primary.main },
  }),
  { name: 'NDToolSheet' },
)

// ToolSheet is the sheet a Now Playing tool opens from the bottom: a title
// over a list of choices, the current one checked. Picking one calls
// onChoose with its value and closes the sheet.
const ToolSheet = ({ open, onClose, title, choices, selected, onChoose }) => {
  const classes = useStyles()
  return (
    <Drawer
      anchor="bottom"
      open={open}
      onClose={onClose}
      PaperProps={{ className: classes.paper, 'aria-label': title }}
    >
      <Typography component="h2" className={classes.title}>
        {title}
      </Typography>
      <MenuList autoFocusItem={open}>
        {choices.map(({ value, label, disabled }) => (
          <MenuItem
            key={label}
            role="menuitemradio"
            aria-checked={value === selected}
            selected={value === selected}
            disabled={disabled}
            className={classes.item}
            onClick={() => {
              onChoose(value)
              onClose()
            }}
          >
            <ListItemIcon className={classes.check}>
              {value === selected ? <CheckIcon fontSize="small" /> : <span />}
            </ListItemIcon>
            <ListItemText primary={label} />
          </MenuItem>
        ))}
      </MenuList>
    </Drawer>
  )
}

export default ToolSheet
