import { useCallback, useEffect, useState } from 'react'
import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'

const isAdmin = () => localStorage.getItem('role') === 'admin'

// useLibraryEditing tells the admin whether editing the Library's Files is on, and which
// libraries' folders are writable, as the server found at startup (ADR-0011). It is always off
// for a Listener, and off until the server answers.
export const useLibraryEditing = () => {
  const admin = isAdmin()
  const [state, setState] = useState({ loaded: !admin, libraries: [] })

  useEffect(() => {
    if (!admin) return
    let active = true
    httpClient(`${REST_URL}/libraryEditing`)
      .then(({ json }) => {
        if (!active) return
        setState({
          loaded: true,
          enabled: json?.enabled === true,
          libraries: json?.libraries || [],
        })
      })
      .catch(() => active && setState({ loaded: true, libraries: [] }))
    return () => {
      active = false
    }
  }, [admin])

  const isWritable = useCallback(
    (id) =>
      admin && state.libraries.some((l) => l.id === id && l.writable === true),
    [admin, state.libraries],
  )

  return {
    loaded: state.loaded,
    enabled: admin && state.enabled === true,
    isWritable,
  }
}
