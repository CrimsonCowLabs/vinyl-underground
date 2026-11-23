import React, { useEffect, useRef } from 'react'
import { useTranslate } from 'react-admin'
import { runVisual } from './runVisual'
import { visualMode } from './visualModes'

// A phone (a coarse pointer) draws at 30 frames a second, to save battery;
// anything else at the screen's rate, up to 60
const frameRate = () =>
  window.matchMedia?.('(pointer: coarse)')?.matches ? 30 : 60

// VisualCanvas plays the visual `mode` on a canvas, reading `analyser`, for
// as long as it is mounted. Unmounting it (another tab, Now Playing closed)
// stops the drawing; runVisual also stops it in the background.
const VisualCanvas = ({ mode, analyser, theme, playing, className }) => {
  const translate = useTranslate()
  const canvasRef = useRef(null)
  const runRef = useRef(null)
  const latest = useRef({ theme, playing })
  latest.current = { theme, playing }

  useEffect(() => {
    const run = runVisual(canvasRef.current, visualMode(mode), {
      analyser,
      theme: latest.current.theme,
      playing: latest.current.playing,
      fps: frameRate(),
    })
    runRef.current = run
    return () => {
      run.stop()
      runRef.current = null
    }
  }, [mode, analyser])

  useEffect(() => {
    runRef.current?.setTheme(theme)
  }, [theme])

  useEffect(() => {
    runRef.current?.setPlaying(playing)
  }, [playing])

  return (
    <canvas
      ref={canvasRef}
      className={className}
      role="img"
      aria-label={translate('nowPlaying.visuals.canvas', {
        name: translate(`nowPlaying.visuals.modes.${mode}`),
      })}
    />
  )
}

export default VisualCanvas
