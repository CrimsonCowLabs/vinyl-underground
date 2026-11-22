import React, { useEffect, useRef, useState } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { useTranslate } from 'react-admin'
import { ButtonBase, IconButton, Typography } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import clsx from 'clsx'
import CloseIcon from '@material-ui/icons/Close'
import DragHandleIcon from '@material-ui/icons/DragHandle'
import { moveInQueue, playFromQueue, removeFromQueue } from '../../actions'
import { formatDuration } from '../../utils'
import { vuPalette } from '../vuPalette'
import { dropIndex } from './dropIndex'

const ROW_GAP = 2

const useStyles = makeStyles(
  (theme) => {
    const vu = vuPalette(theme)
    return {
      scroller: {
        position: 'absolute',
        inset: 0,
        overflowY: 'auto',
        overscrollBehavior: 'contain',
      },
      head: {
        display: 'block',
        padding: '2px 4px 8px',
        color: theme.palette.text.secondary,
        lineHeight: 1.6,
      },
      list: {
        listStyle: 'none',
        margin: 0,
        padding: 0,
        display: 'grid',
        gap: ROW_GAP,
      },
      row: {
        display: 'grid',
        gridTemplateColumns: '40px minmax(0, 1fr) 40px',
        alignItems: 'center',
        minHeight: 56,
        borderRadius: 8,
        position: 'relative',
        '&:hover': { backgroundColor: vu.surface2 },
      },
      current: {
        backgroundColor: vu.accentSoft,
        '&:hover': { backgroundColor: vu.accentSoft },
        '& $title': { color: theme.palette.primary.main },
      },
      dragging: {
        zIndex: 1,
        backgroundColor: vu.surface3,
        boxShadow: '0 6px 18px rgba(0, 0, 0, 0.3)',
        '&:hover': { backgroundColor: vu.surface3 },
      },
      shifting: { transition: 'transform 0.15s ease-out' },
      grip: {
        alignSelf: 'stretch',
        color: theme.palette.text.disabled,
        cursor: 'grab',
        // The handle takes the touch for dragging, not for scrolling
        touchAction: 'none',
        borderRadius: 8,
      },
      play: {
        display: 'grid',
        gridTemplateColumns: '40px minmax(0, 1fr) auto',
        alignItems: 'center',
        gap: 10,
        padding: '8px 4px',
        justifyContent: 'stretch',
        textAlign: 'left',
        borderRadius: 8,
        minWidth: 0,
      },
      cover: {
        width: 40,
        height: 40,
        borderRadius: 4,
        objectFit: 'cover',
        display: 'block',
        backgroundColor: vu.surface2,
      },
      names: { minWidth: 0 },
      title: {
        display: 'block',
        fontSize: 14,
        fontWeight: 700,
        whiteSpace: 'nowrap',
        overflow: 'hidden',
        textOverflow: 'ellipsis',
      },
      sub: {
        display: 'block',
        fontSize: 12.5,
        color: theme.palette.text.secondary,
        whiteSpace: 'nowrap',
        overflow: 'hidden',
        textOverflow: 'ellipsis',
      },
      duration: { fontVariantNumeric: 'tabular-nums' },
      remove: {
        color: theme.palette.text.secondary,
        padding: 8,
        '& svg': { width: 20, height: 20 },
      },
    }
  },
  { name: 'NDNowPlayingQueue' },
)

// m:ss, as the rest of Now Playing shows times
const duration = (seconds) => formatDuration(seconds).replace(/^0(?=\d:)/, '')

const titleOf = (item) => item.song?.title || item.name || ''
const artistOf = (item) => item.song?.artist || item.singer || ''

// The index of the Track playing or, before playback starts after a reload,
// the one queued to play
const selectQueue = (state) => {
  const { queue, current, savedPlayIndex } = state.player
  const playing = queue.findIndex((item) => item.uuid === current?.uuid)
  return { queue, playing: playing >= 0 ? playing : savedPlayIndex || 0 }
}

// useDrag moves a queued Track by its handle: a pointer drag (mouse, pen or
// finger), the dragged row following it and the rows it passes making way,
// or the arrow keys. Each drop is one moveInQueue.
const useDrag = (count, scrollerRef) => {
  const dispatch = useDispatch()
  const [drag, setDrag] = useState(null)

  const offset = (d, e) =>
    e.clientY - d.startY + (scrollerRef.current?.scrollTop || 0) - d.startScroll

  const handlers = (index) => ({
    onPointerDown: (e) => {
      if (e.button > 0) return
      e.preventDefault()
      e.currentTarget.setPointerCapture?.(e.pointerId)
      const row = e.currentTarget.closest('li')
      setDrag({
        from: index,
        startY: e.clientY,
        startScroll: scrollerRef.current?.scrollTop || 0,
        rowHeight: (row?.getBoundingClientRect().height || 56) + ROW_GAP,
        dy: 0,
      })
    },
    onPointerMove: (e) => {
      if (!drag) return
      // Near the top or bottom edge, the list scrolls to reach further
      const box = scrollerRef.current
      if (box) {
        const { top, bottom } = box.getBoundingClientRect()
        if (e.clientY < top + 40) box.scrollTop -= 8
        else if (e.clientY > bottom - 40) box.scrollTop += 8
      }
      setDrag({ ...drag, dy: offset(drag, e) })
    },
    onPointerUp: (e) => {
      if (!drag) return
      const to = dropIndex(drag.from, offset(drag, e), drag.rowHeight, count)
      setDrag(null)
      if (to !== drag.from) dispatch(moveInQueue(drag.from, to))
    },
    onPointerCancel: () => setDrag(null),
    onKeyDown: (e) => {
      const to =
        e.key === 'ArrowUp' ? index - 1 : e.key === 'ArrowDown' ? index + 1 : -1
      if (to < 0 || to >= count) return
      e.preventDefault()
      dispatch(moveInQueue(index, to))
    },
  })

  // How far each row is moved while dragging: the dragged one follows the
  // pointer, and the ones it has passed shift a row the other way
  const shift = (index) => {
    if (!drag) return 0
    if (index === drag.from) return drag.dy
    const to = dropIndex(drag.from, drag.dy, drag.rowHeight, count)
    if (drag.from < index && index <= to) return -drag.rowHeight
    if (to <= index && index < drag.from) return drag.rowHeight
    return 0
  }

  return { dragging: drag?.from, handlers, shift }
}

// QueueView is Now Playing's Queue tab: the play queue, the Track playing
// marked. A Track is played by tapping it, moved by dragging its handle (or
// with the arrow keys on it), and taken out with its ✕. Each edit goes to the
// player through the queue in the store, which the player package follows.
const QueueView = ({ playback, dj }) => {
  const classes = useStyles()
  const translate = useTranslate()
  const dispatch = useDispatch()
  const { queue, playing } = useSelector(selectQueue)
  const scroller = useRef(null)
  const { dragging, handlers, shift } = useDrag(queue.length, scroller)

  // Starts with the Track playing at the top, and keeps it in view as the
  // queue moves on
  const playingUuid = queue[playing]?.uuid
  useEffect(() => {
    const box = scroller.current
    const row = box?.querySelector('[aria-current="true"]')
    if (row) box.scrollTop = row.offsetTop - box.firstChild.offsetHeight
  }, [playingUuid])

  const play = (index) => {
    if (index !== playing) dispatch(playFromQueue(index))
    else if (!playback.playing) playback.togglePlay()
  }

  return (
    <div ref={scroller} className={classes.scroller} data-no-swipe>
      <Typography variant="overline" component="h3" className={classes.head}>
        {dj
          ? translate('nowPlaying.queue.upNextStation', { name: dj.name })
          : translate('nowPlaying.queue.upNext')}
      </Typography>
      <ul className={classes.list}>
        {queue.map((item, index) => {
          const title = titleOf(item)
          const dy = shift(index)
          return (
            <li
              key={item.uuid}
              className={clsx(
                classes.row,
                index === playing && classes.current,
                index === dragging && classes.dragging,
                dragging != null && index !== dragging && classes.shifting,
              )}
              style={dy ? { transform: `translateY(${dy}px)` } : undefined}
              aria-current={index === playing ? 'true' : undefined}
            >
              <ButtonBase
                className={classes.grip}
                aria-label={translate('nowPlaying.queue.move', {
                  name: title,
                })}
                {...handlers(index)}
              >
                <DragHandleIcon />
              </ButtonBase>
              <ButtonBase className={classes.play} onClick={() => play(index)}>
                <img className={classes.cover} src={item.cover} alt="" />
                <span className={classes.names}>
                  <span className={classes.title}>{title}</span>
                  <span className={classes.sub}>{artistOf(item)}</span>
                </span>
                {item.duration > 0 && (
                  <span className={clsx(classes.sub, classes.duration)}>
                    {duration(item.duration)}
                  </span>
                )}
              </ButtonBase>
              {queue.length > 1 ? (
                <IconButton
                  className={classes.remove}
                  aria-label={translate('nowPlaying.queue.remove', {
                    name: title,
                  })}
                  onClick={() => dispatch(removeFromQueue(index))}
                >
                  <CloseIcon />
                </IconButton>
              ) : (
                <span />
              )}
            </li>
          )
        })}
      </ul>
    </div>
  )
}

export default QueueView
