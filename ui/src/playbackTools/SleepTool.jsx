import React, { useEffect, useState } from 'react'
import { useTranslate } from 'react-admin'
import TimerIcon from '@material-ui/icons/Timer'
import { NowPlayingToolButton } from '../nowPlaying/NowPlayingToolbar'
import { sleepTimer } from './sleepTimer'
import ToolSheet from './ToolSheet'

const MINUTES = [15, 30, 45, 60]
const END_OF_TRACK = 'endOfTrack'
const OFF = 'off'

// m:ss
const formatRemaining = (seconds) =>
  `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`

const read = () => ({
  ...sleepTimer.state(),
  remaining: sleepTimer.remainingSeconds(),
})

// useSleepTimer follows the sleep timer, and the time it has left each second
// while it is on
const useSleepTimer = () => {
  const [timer, setTimer] = useState(read)
  useEffect(() => {
    const update = () => setTimer(read())
    update()
    return sleepTimer.subscribe(update)
  }, [])
  useEffect(() => {
    if (!timer.mode) return
    const tick = setInterval(() => setTimer(read()), 1000)
    return () => clearInterval(tick)
  }, [timer.mode])
  return timer
}

// SleepTool is the toolbar's Sleep timer: it shows the time left while on,
// and opens a sheet to set it or turn it off.
const SleepTool = () => {
  const translate = useTranslate()
  const timer = useSleepTimer()
  const [open, setOpen] = useState(false)
  const on = !!timer.mode
  const selected =
    timer.mode === END_OF_TRACK ? END_OF_TRACK : (timer.minutes ?? OFF)

  const choices = [
    ...MINUTES.map((minutes) => ({
      value: minutes,
      label: translate('nowPlaying.sleep.minutes', { smart_count: minutes }),
    })),
    {
      value: END_OF_TRACK,
      label: translate('nowPlaying.sleep.endOfTrack'),
      disabled: open && !sleepTimer.trackEndKnown(),
    },
    ...(on ? [{ value: OFF, label: translate('nowPlaying.sleep.off') }] : []),
  ]

  const choose = (value) => {
    if (value === OFF) sleepTimer.cancel()
    else if (value === END_OF_TRACK) sleepTimer.startEndOfTrack()
    else sleepTimer.start(value)
  }

  return (
    <>
      <NowPlayingToolButton
        icon={<TimerIcon />}
        label={translate('nowPlaying.sleep.label')}
        value={
          on && timer.remaining !== null
            ? formatRemaining(timer.remaining)
            : undefined
        }
        active={on}
        aria-haspopup="true"
        onClick={() => setOpen(true)}
      />
      <ToolSheet
        open={open}
        onClose={() => setOpen(false)}
        title={translate('nowPlaying.sleep.title')}
        choices={choices}
        selected={selected}
        onChoose={choose}
      />
    </>
  )
}

export default SleepTool
