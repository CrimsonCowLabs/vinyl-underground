import { useEffect, useState } from 'react'
import { useSelector } from 'react-redux'
import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'
import { latestBlurb } from '../station/stationState'

// useStationDJ is the Station on air's DJ, as { name, blurb }: the DJ's name
// and the Blurb it showed as the Track playing started, if any. It is null
// off a Station. A Station started before the DJ's name was kept with it
// (restored from an older session) asks the server for the name.
export const useStationDJ = () => {
  const djId = useSelector((state) => state.station?.djId)
  const kept = useSelector((state) => state.station?.djName)
  const blurb = useSelector(latestBlurb)
  const [asked, setAsked] = useState(null) // { djId, name }

  useEffect(() => {
    if (!djId || kept) return
    let live = true
    httpClient(`${REST_URL}/station/dj/${djId}`)
      .then(({ json }) => {
        if (live && json?.name) setAsked({ djId, name: json.name })
      })
      .catch(() => {}) // the card shows once a Blurb names the DJ
    return () => {
      live = false
    }
  }, [djId, kept])

  if (!djId) return null
  const name =
    kept || (asked?.djId === djId && asked.name) || blurb?.djName || ''
  return { name, blurb }
}
