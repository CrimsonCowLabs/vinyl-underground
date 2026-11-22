import React from 'react'
import { makeStyles } from '@material-ui/core/styles'
import clsx from 'clsx'
import config from '../config'

const useStyles = makeStyles(
  (theme) => ({
    root: {
      position: 'relative',
      // Leaves room on the right for the record sliding out
      width: 'min(68vw, 38vh, 420px)',
      aspectRatio: '1 / 1',
      marginRight: '12%',
    },
    record: {
      position: 'absolute',
      top: '3%',
      left: '3%',
      width: '94%',
      height: '94%',
      borderRadius: '50%',
      background: [
        'radial-gradient(circle, transparent 0 4%, #000 4% 5%, transparent 5%)',
        `radial-gradient(circle, ${theme.palette.primary.main} 0 17%, #0d0b0a 17.5% 19%, transparent 19%)`,
        'repeating-radial-gradient(circle, #141210 0 1.5px, #1f1c19 1.5px 3px)',
        '#111',
      ].join(', '),
      boxShadow: '0 10px 30px rgba(0, 0, 0, 0.45)',
      transition: 'left 0.6s cubic-bezier(0.2, 0.8, 0.2, 1)',
    },
    out: { left: '20%' },
    spinning: {
      animation: '$spin 3.6s linear infinite',
      '@media (prefers-reduced-motion: reduce)': { animation: 'none' },
    },
    '@keyframes spin': { to: { transform: 'rotate(360deg)' } },
    sleeve: {
      position: 'relative',
      display: 'block',
      width: '100%',
      height: '100%',
      objectFit: 'cover',
      borderRadius: 6,
      backgroundColor: theme.palette.background.paper,
      boxShadow: '0 18px 40px rgba(0, 0, 0, 0.45)',
    },
  }),
  { name: 'NDNowPlayingArt' },
)

// RecordArt is the album art as a sleeve, with the record sliding partly out
// of it while the Track plays (and turning, unless motion is reduced or cover
// animation is off in the settings).
const RecordArt = ({ cover, alt, playing }) => {
  const classes = useStyles()
  return (
    <div className={classes.root}>
      <div
        aria-hidden="true"
        className={clsx(
          classes.record,
          playing && classes.out,
          playing && config.enableCoverAnimation && classes.spinning,
        )}
      />
      {cover ? (
        <img className={classes.sleeve} src={cover} alt={alt} />
      ) : (
        <div className={classes.sleeve} role="img" aria-label={alt} />
      )}
    </div>
  )
}

export default RecordArt
