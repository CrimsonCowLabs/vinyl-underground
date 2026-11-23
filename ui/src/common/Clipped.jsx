import React from 'react'
import { makeStyles } from '@material-ui/core/styles'
import { OverflowTooltip } from './OverflowTooltip'

const useStyles = makeStyles(
  {
    clipped: {
      display: 'block',
      overflow: 'hidden',
      textOverflow: 'ellipsis',
      whiteSpace: 'nowrap',
    },
  },
  { name: 'NDClipped' },
)

// Clipped shows its text (or children) on one line, cut short with an
// ellipsis if it doesn't fit, with the full text on hover when it is cut.
export const Clipped = ({ title, children }) => {
  const classes = useStyles()
  if (!title && !children) return null
  return (
    <OverflowTooltip title={title || ''}>
      <span className={classes.clipped}>{children || title}</span>
    </OverflowTooltip>
  )
}
