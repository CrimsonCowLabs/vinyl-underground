import React from 'react'
import { useSelector } from 'react-redux'
import { useTranslate } from 'react-admin'
import { makeStyles } from '@material-ui/core/styles'
import { latestBlurb } from '../station/stationState'
import { vuPalette } from './vuPalette'

const useStyles = makeStyles(
  (theme) => {
    const vu = vuPalette(theme)
    return {
      root: {
        display: 'grid',
        gridTemplateColumns: '40px minmax(0, 1fr)',
        gap: theme.spacing(1.25),
        padding: theme.spacing(1.25, 1.5),
        borderRadius: 12,
        backgroundColor: vu.surface2,
        borderLeft: `3px solid ${theme.palette.primary.main}`,
      },
      avatar: {
        width: 40,
        height: 40,
        borderRadius: '50%',
        backgroundColor: theme.palette.primary.main,
        color: theme.palette.primary.contrastText,
        display: 'grid',
        placeItems: 'center',
        fontWeight: 800,
        fontSize: 14,
      },
      body: { minWidth: 0 },
      name: {
        fontSize: 13.5,
        fontWeight: 800,
        display: 'flex',
        gap: 6,
        alignItems: 'center',
        minHeight: 40,
      },
      nameWithBlurb: { minHeight: 0 },
      tag: {
        fontSize: 10,
        letterSpacing: '0.12em',
        padding: '1px 6px',
        borderRadius: 4,
        backgroundColor: vu.surface3,
        color: theme.palette.text.secondary,
      },
      blurb: {
        marginTop: 2,
        fontSize: 13.5,
        lineHeight: 1.4,
        color: theme.palette.text.secondary,
      },
    }
  },
  { name: 'NDNowPlayingStationCard' },
)

const initials = (name) =>
  name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((word) => word[0].toUpperCase())
    .join('')

// StationCard shows, on a Station, its DJ and the Blurb the DJ showed as the
// Track playing started (Blurbs on, and written in time). It updates as the
// Tracks change.
const StationCard = () => {
  const classes = useStyles()
  const translate = useTranslate()
  const onAir = useSelector((state) => !!state.station?.djId)
  const blurb = useSelector(latestBlurb)
  const djName = useSelector((state) => state.station?.djName) || blurb?.djName

  if (!onAir || !djName) return null
  return (
    <section
      className={classes.root}
      aria-label={translate('nowPlaying.djCard')}
      aria-live="polite"
    >
      <div className={classes.avatar} aria-hidden="true">
        {initials(djName)}
      </div>
      <div className={classes.body}>
        <div
          className={`${classes.name} ${blurb ? classes.nameWithBlurb : ''}`}
        >
          {djName}
          <span className={classes.tag}>{translate('nowPlaying.dj')}</span>
        </div>
        {blurb && <div className={classes.blurb}>{blurb.text}</div>}
      </div>
    </section>
  )
}

export default StationCard
