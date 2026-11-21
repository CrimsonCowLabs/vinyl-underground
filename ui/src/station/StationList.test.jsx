import React from 'react'
import { Provider } from 'react-redux'
import { combineReducers, legacy_createStore as createStore } from 'redux'
import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
} from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ThemeProvider, createTheme } from '@material-ui/core/styles'
import config from '../config'
import { playerReducer } from '../reducers'
import { setPlayMode } from '../actions'
import { stationReducer } from './stationState'
import StationList from './StationList'

const notify = vi.fn()
const httpClient = vi.fn()

vi.mock('../config', () => ({
  default: { enableStationThemes: false },
}))

vi.mock('react-admin', () => ({
  Title: ({ title }) => <h1>{title}</h1>,
  useTranslate: () => (key) => key,
  useNotify: () => notify,
}))

vi.mock('../dataProvider', () => ({
  httpClient: (...args) => httpClient(...args),
}))

const djs = [
  { id: 'night-owl', name: 'Night Owl', description: 'Low-key, late-night.' },
  { id: 'crate-digger', name: 'The Crate Digger', description: 'Deep cuts.' },
  { id: 'sunny', name: 'Sunny', description: 'Upbeat.' },
]

const firstTracks = [
  { id: 'mf-1', title: 'Blue in Green', artist: 'Miles Davis' },
  { id: 'mf-2', title: 'Naima', artist: 'John Coltrane' },
]

const httpError = (status, code) =>
  Object.assign(new Error(code), { status, body: { error: code } })

const renderPage = () => {
  const store = createStore(
    combineReducers({ player: playerReducer, station: stationReducer }),
  )
  render(
    <Provider store={store}>
      <ThemeProvider theme={createTheme()}>
        <StationList />
      </ThemeProvider>
    </Provider>,
  )
  return store
}

describe('StationList', () => {
  beforeEach(() => {
    httpClient.mockReset()
    notify.mockReset()
    config.enableStationThemes = false
    httpClient.mockImplementation((url) =>
      url.endsWith('/station/dj')
        ? Promise.resolve({ json: djs })
        : Promise.resolve({ json: { tracks: firstTracks } }),
    )
  })
  afterEach(cleanup)

  it('lists the DJs a Listener can start a Station with', async () => {
    renderPage()
    expect(await screen.findByText('Night Owl')).toBeInTheDocument()
    expect(screen.getByText('The Crate Digger')).toBeInTheDocument()
    expect(screen.getByText('Sunny')).toBeInTheDocument()
    expect(screen.getByText('Deep cuts.')).toBeInTheDocument()
  })

  it('starts a Station: its Tracks replace the play queue', async () => {
    const store = renderPage()
    await screen.findByText('Night Owl')

    fireEvent.click(
      screen.getByRole('button', {
        name: 'resources.station.actions.start Night Owl',
      }),
    )

    await waitFor(() => expect(store.getState().station.djId).toBe('night-owl'))
    const [url, opts] = httpClient.mock.calls[1]
    expect(url).toMatch(/\/station\/next$/)
    expect(JSON.parse(opts.body)).toEqual({ dj: 'night-owl', count: 5 })
    expect(store.getState().player.queue.map((i) => i.trackId)).toEqual([
      'mf-1',
      'mf-2',
    ])
    expect(screen.getByText('resources.station.onAir')).toBeInTheDocument()
  })

  it('hides the Theme field when no LLM is configured', async () => {
    renderPage()
    await screen.findByText('Night Owl')
    expect(
      screen.queryByLabelText('resources.station.fields.theme'),
    ).not.toBeInTheDocument()
  })

  it('starts a Station with the Theme the Listener gives, when an LLM is configured', async () => {
    config.enableStationThemes = true
    const store = renderPage()
    await screen.findByText('Night Owl')
    fireEvent.change(screen.getByLabelText('resources.station.fields.theme'), {
      target: { value: 'late-night drive' },
    })
    fireEvent.click(
      screen.getByRole('button', {
        name: 'resources.station.actions.start Night Owl',
      }),
    )
    await waitFor(() => expect(store.getState().station.djId).toBe('night-owl'))
    const [, opts] = httpClient.mock.calls[1]
    expect(JSON.parse(opts.body)).toEqual({
      dj: 'night-owl',
      count: 5,
      theme: 'late-night drive',
    })
    expect(store.getState().station.theme).toBe('late-night drive')
  })

  it('plays the Station in order, as shuffle or repeat would replay Tracks', async () => {
    const store = renderPage()
    store.dispatch(setPlayMode('shufflePlay'))
    await screen.findByText('Sunny')

    fireEvent.click(
      screen.getByRole('button', {
        name: 'resources.station.actions.start Sunny',
      }),
    )

    await waitFor(() => expect(store.getState().station.djId).toBe('sunny'))
    expect(store.getState().player.mode).toBe('order')
  })

  it('says so when the Library has nothing to play', async () => {
    httpClient.mockImplementation((url) =>
      url.endsWith('/station/dj')
        ? Promise.resolve({ json: djs })
        : Promise.reject(httpError(422, 'noTracks')),
    )
    const store = renderPage()
    await screen.findByText('Sunny')

    fireEvent.click(
      screen.getByRole('button', {
        name: 'resources.station.actions.start Sunny',
      }),
    )

    await waitFor(() =>
      expect(notify).toHaveBeenCalledWith('resources.station.errors.noTracks', {
        type: 'warning',
      }),
    )
    expect(store.getState().station.djId).toBeNull()
  })
})
