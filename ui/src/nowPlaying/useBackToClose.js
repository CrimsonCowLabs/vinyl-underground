import { useCallback, useEffect, useRef } from 'react'

const KEY = 'nowPlaying'
const ours = () => !!window.history.state?.[KEY]
const pushEntry = () =>
  window.history.pushState({ ...window.history.state, [KEY]: true }, '')

// The popstate caused by the view taking its own entry back after closing
// some other way (its button, Esc, the down gesture). It isn't a request to
// close, e.g. when the view reopens before that back lands.
let takenBack = null
const noteTakenBack = (e) => {
  takenBack = e
  window.removeEventListener('popstate', noteTakenBack)
}

// useBackToClose makes Android's and the browser's back close the view while
// it is open, rather than leave the page: opening it pushes a history entry
// with the same URL (the hash router ignores it), back pops it and closes the
// view, and closing it any other way takes the entry back off.
//
// It returns leave(path, router), for navigating from the view: the view's
// entry becomes the destination, so back from there returns to the page that
// was under the view.
export const useBackToClose = (open, close) => {
  const closeRef = useRef(close)
  closeRef.current = close

  useEffect(() => {
    if (!open) return
    if (!ours()) pushEntry()
    const onPopState = (e) => {
      if (e === takenBack) {
        pushEntry() // reopened before the last close's back landed
        return
      }
      closeRef.current()
    }
    window.addEventListener('popstate', onPopState)
    return () => {
      window.removeEventListener('popstate', onPopState)
      if (ours()) {
        // Registered before any reopened view's listener, so it runs first
        window.addEventListener('popstate', noteTakenBack)
        window.history.back()
      }
    }
  }, [open])

  return useCallback((path, router) => {
    if (ours()) {
      window.history.replaceState({ ...window.history.state, [KEY]: false }, '')
      router.replace(path)
    } else {
      router.push(path)
    }
  }, [])
}
