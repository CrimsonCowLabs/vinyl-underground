import React from 'react'
import PropTypes from 'prop-types'
import { useTranslate } from 'react-admin'
import { Chip, Tooltip } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import clsx from 'clsx'
import { ripSourceURL } from './ripSource'

const useStyles = makeStyles(
  (theme) => ({
    chip: {
      height: '1.5em',
      marginLeft: theme.spacing(1),
      fontSize: '0.7rem',
      verticalAlign: 'text-bottom',
      cursor: 'default',
    },
    url: {
      display: 'block',
      wordBreak: 'break-all',
    },
  }),
  { name: 'NDRippedBadge' },
)

// RippedBadge marks a Rip (see GLOSSARY.md) and renders nothing for any other Track.
export const RippedBadge = ({ record, className }) => {
  const translate = useTranslate()
  const classes = useStyles()
  const url = ripSourceURL(record)
  if (!url) {
    return null
  }
  const source = record.source || url.hostname
  const from = translate('resources.song.ripped.from', { source })
  return (
    <Tooltip
      title={
        <>
          {from}
          <span className={classes.url}>{record.sourceUrl}</span>
        </>
      }
    >
      <Chip
        className={clsx(classes.chip, className)}
        size="small"
        variant="outlined"
        label={translate('resources.song.ripped.label')}
        aria-label={from}
        tabIndex={0}
      />
    </Tooltip>
  )
}

RippedBadge.propTypes = {
  record: PropTypes.object,
  className: PropTypes.string,
}
