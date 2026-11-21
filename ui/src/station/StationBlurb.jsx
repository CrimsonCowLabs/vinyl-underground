import React, { useEffect, useRef, useState } from 'react'
import { useSelector } from 'react-redux'
import { useTranslate } from 'react-admin'
import { IconButton, Snackbar, SnackbarContent } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import CloseIcon from '@material-ui/icons/Close'
import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'
import config from '../config'

// How long a Blurb stays on screen.
export const BLURB_MS = 15000

const useStyles = makeStyles((theme) => ({
  root: { maxWidth: 560 },
  dj: { fontWeight: theme.typography.fontWeightBold, display: 'block' },
}))

// fetchStationBlurb asks for the Blurb the DJ shows as the Station changes
// from the Track after into track. It is null when there is none yet.
export const fetchStationBlurb = (djId, track, after) =>
  httpClient(
    `${REST_URL}/station/blurb?${new URLSearchParams({ dj: djId, track, after })}`,
  ).then(({ json }) => (json?.text ? json : null))

// StationBlurb shows the DJ's Blurb on screen at each Track change on the
// Station on air. Blurbs are written ahead on the server; one that isn't ready
// when its Track starts is simply not shown, and the Track plays on regardless.
const StationBlurb = () => {
  const classes = useStyles()
  const translate = useTranslate()
  const station = useSelector((state) => state.station)
  const current = useSelector((state) => state.player.current)
  const [blurb, setBlurb] = useState(null)
  const last = useRef(null) // the Track playing before, and the Station it played on

  const onAir = station?.djId ? station : null
  const uuid = current?.uuid
  const trackId = current?.trackId

  useEffect(() => {
    if (!uuid || last.current?.uuid === uuid) return
    const before = last.current
    last.current = { uuid, trackId, djId: onAir?.djId, session: onAir?.session }
    setBlurb(null)
    if (
      !config.enableStationBlurbs ||
      !onAir ||
      !before?.djId ||
      before.djId !== onAir.djId ||
      before.session !== onAir.session
    ) {
      return
    }
    fetchStationBlurb(onAir.djId, trackId, before.trackId)
      .then((res) => {
        // The Track may have changed again meanwhile.
        if (res && last.current?.uuid === uuid) setBlurb(res)
      })
      .catch((e) => {
        // eslint-disable-next-line no-console
        console.debug('No Station Blurb:', e)
      })
  }, [uuid, trackId, onAir?.djId, onAir?.session]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!blurb) return
    const hide = setTimeout(() => setBlurb(null), BLURB_MS)
    return () => clearTimeout(hide)
  }, [blurb])

  if (!blurb) return null
  return (
    <Snackbar open anchorOrigin={{ vertical: 'top', horizontal: 'center' }}>
      <SnackbarContent
        className={classes.root}
        role="status"
        message={
          <span>
            <span className={classes.dj}>{blurb.djName}</span>
            {blurb.text}
          </span>
        }
        action={
          <IconButton
            size="small"
            color="inherit"
            aria-label={translate('ra.action.close')}
            onClick={() => setBlurb(null)}
          >
            <CloseIcon fontSize="small" />
          </IconButton>
        }
      />
    </Snackbar>
  )
}

export default StationBlurb
