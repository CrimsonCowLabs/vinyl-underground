import React from 'react'
import { Provider } from 'react-redux'
import { combineReducers, legacy_createStore as createStore } from 'redux'
import { render, screen, act, cleanup } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ThemeProvider, createTheme } from '@material-ui/core/styles'
import { playerReducer } from '../reducers'
import {
  startStation,
  stationPicking,
  stationPickingFailed,
  stationReducer,
} from './stationState'
import StationPicking from './StationPicking'

vi.mock('react-admin', () => ({
  useTranslate: () => (key) => key,
}))

const nightOwl = { id: 'night-owl', name: 'Night Owl' }
const tracks = [{ id: 'mf-1', title: 'Blue in Green', artist: 'Miles Davis' }]

describe('StationPicking', () => {
  let store

  beforeEach(() => {
    store = createStore(
      combineReducers({ player: playerReducer, station: stationReducer }),
    )
    render(
      <Provider store={store}>
        <ThemeProvider theme={createTheme()}>
          <StationPicking />
        </ThemeProvider>
      </Provider>,
    )
  })
  afterEach(cleanup)

  it('shows nothing while no Station is starting', () => {
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
    expect(
      screen.queryByText('resources.station.picking'),
    ).not.toBeInTheDocument()
  })

  it("shows the DJ's name and a spinner while the DJ picks the first songs", () => {
    act(() => {
      store.dispatch(stationPicking(nightOwl))
    })

    expect(screen.getByText('Night Owl')).toBeInTheDocument()
    expect(screen.getByText('resources.station.picking')).toBeInTheDocument()
    expect(screen.getByRole('progressbar')).toBeInTheDocument()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('goes once the Station starts', () => {
    act(() => {
      store.dispatch(stationPicking(nightOwl))
      store.dispatch(startStation('night-owl', tracks))
    })

    expect(
      screen.queryByText('resources.station.picking'),
    ).not.toBeInTheDocument()
  })

  it("goes if the Station couldn't start", () => {
    act(() => {
      store.dispatch(stationPicking(nightOwl))
      store.dispatch(stationPickingFailed())
    })

    expect(
      screen.queryByText('resources.station.picking'),
    ).not.toBeInTheDocument()
  })
})
