import React from 'react'
import { Provider } from 'react-redux'
import { combineReducers, legacy_createStore as createStore } from 'redux'
import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
  act,
} from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ThemeProvider, createTheme } from '@material-ui/core/styles'
import config from '../config'
import { playerReducer } from '../reducers'
import { playTracks, setPlayMode } from '../actions'
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
  {
    id: 'night-owl',
    name: 'Night Owl',
    description: 'Low-key, late-night.',
    builtIn: true,
  },
  {
    id: 'crate-digger',
    name: 'The Crate Digger',
    description: 'Deep cuts.',
    builtIn: true,
  },
  { id: 'sunny', name: 'Sunny', description: 'Upbeat.', builtIn: true },
]

const mellow = {
  id: 'dj-1',
  name: 'Mr. Mellow',
  description: 'Slow, soft songs.',
  builtIn: false,
}

// serveDJs answers the DJ list with the DJs given (built-in ones by default),
// and answers saving, deleting and starting a Station with what the server
// would.
const serveDJs = (list = djs, overrides = {}) =>
  httpClient.mockImplementation((url, opts = {}) => {
    const method = opts.method || 'GET'
    const key = `${method} ${url.replace(/^.*\/station/, '')}`
    if (overrides[key]) return overrides[key](opts)
    if (key === 'GET /dj') return Promise.resolve({ json: list })
    return Promise.resolve({ json: { tracks: firstTracks } })
  })

const calls = (method, path) =>
  httpClient.mock.calls.filter(
    ([url, opts = {}]) =>
      (opts.method || 'GET') === method && url.endsWith(`/station${path}`),
  )

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
    serveDJs()
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
    const [[, opts]] = calls('POST', '/next')
    expect(JSON.parse(opts.body)).toEqual({
      dj: 'night-owl',
      count: 5,
      start: true,
    })
    expect(store.getState().player.queue.map((i) => i.trackId)).toEqual([
      'mf-1',
      'mf-2',
    ])
    expect(screen.getByText('resources.station.onAir')).toBeInTheDocument()
  })

  it('says the DJ is picking the first songs until the Station starts', async () => {
    let answer
    serveDJs(djs, {
      'POST /next': () =>
        new Promise((resolve) => {
          answer = resolve
        }),
    })
    const store = renderPage()
    await screen.findByText('Night Owl')

    fireEvent.click(
      screen.getByRole('button', {
        name: 'resources.station.actions.start Night Owl',
      }),
    )

    expect(
      await screen.findByText('resources.station.picking'),
    ).toBeInTheDocument()
    expect(screen.getByRole('progressbar')).toBeInTheDocument()
    expect(store.getState().station.picking).toEqual({
      djId: 'night-owl',
      djName: 'Night Owl',
    })

    answer({ json: { tracks: firstTracks } })
    await waitFor(() => expect(store.getState().station.djId).toBe('night-owl'))
    expect(
      screen.queryByText('resources.station.picking'),
    ).not.toBeInTheDocument()
    expect(store.getState().station.picking).toBeNull()
  })

  it('gives up starting the Station when the Listener plays something else meanwhile', async () => {
    let answer
    serveDJs(djs, {
      'POST /next': () =>
        new Promise((resolve) => {
          answer = resolve
        }),
    })
    const store = renderPage()
    await screen.findByText('Night Owl')
    fireEvent.click(
      screen.getByRole('button', {
        name: 'resources.station.actions.start Night Owl',
      }),
    )
    await screen.findByText('resources.station.picking')

    act(() => {
      store.dispatch(playTracks({ x: { id: 'x', title: 'X' } }))
    })
    expect(store.getState().station.picking).toBeNull()
    answer({ json: { tracks: firstTracks } })

    await waitFor(() =>
      expect(
        screen.getByRole('button', {
          name: 'resources.station.actions.start Night Owl',
        }),
      ).toBeEnabled(),
    )
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(store.getState().station.djId).toBeNull()
    expect(store.getState().player.queue.map((i) => i.trackId)).toEqual(['x'])
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
    const [[, opts]] = calls('POST', '/next')
    expect(JSON.parse(opts.body)).toEqual({
      dj: 'night-owl',
      count: 5,
      theme: 'late-night drive',
      start: true,
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
    expect(store.getState().station.picking).toBeNull()
  })

  describe("a Listener's own DJs", () => {
    const editor = () => screen.getByRole('dialog')
    const fill = (name, description) => {
      fireEvent.change(
        screen.getByLabelText(/^resources.station.dj.fields.name/),
        { target: { value: name } },
      )
      fireEvent.change(
        screen.getByLabelText(/^resources.station.dj.fields.description/),
        { target: { value: description } },
      )
    }
    const save = () =>
      fireEvent.click(screen.getByRole('button', { name: 'ra.action.save' }))

    it('are listed with the built-in ones, which have no edit or delete', async () => {
      serveDJs([...djs, mellow])
      renderPage()
      expect(await screen.findByText('Mr. Mellow')).toBeInTheDocument()
      expect(
        screen.getByRole('button', {
          name: 'resources.station.actions.start Mr. Mellow',
        }),
      ).toBeInTheDocument()
      expect(
        screen.getByRole('button', { name: 'ra.action.edit Mr. Mellow' }),
      ).toBeInTheDocument()
      expect(
        screen.getByRole('button', { name: 'ra.action.delete Mr. Mellow' }),
      ).toBeInTheDocument()
      expect(
        screen.queryByRole('button', { name: 'ra.action.edit Night Owl' }),
      ).not.toBeInTheDocument()
      expect(
        screen.queryByRole('button', { name: 'ra.action.delete Sunny' }),
      ).not.toBeInTheDocument()
    })

    it('creates a DJ from a name and a personality description', async () => {
      let list = djs
      serveDJs(djs, {
        'POST /dj': (opts) => {
          list = [...djs, { ...mellow, ...JSON.parse(opts.body) }]
          serveDJs(list)
          return Promise.resolve({ json: list[3] })
        },
      })
      renderPage()
      await screen.findByText('Night Owl')

      fireEvent.click(
        screen.getByRole('button', { name: 'resources.station.dj.new' }),
      )
      expect(editor()).toBeInTheDocument()
      fill('Mr. Mellow', 'Slow, soft songs.')
      save()

      expect(await screen.findByText('Mr. Mellow')).toBeInTheDocument()
      const [[, opts]] = calls('POST', '/dj')
      expect(JSON.parse(opts.body)).toEqual({
        name: 'Mr. Mellow',
        description: 'Slow, soft songs.',
      })
      await waitFor(() =>
        expect(screen.queryByRole('dialog')).not.toBeInTheDocument(),
      )
    })

    it("can't be saved without a name and a description", async () => {
      renderPage()
      await screen.findByText('Night Owl')
      fireEvent.click(
        screen.getByRole('button', { name: 'resources.station.dj.new' }),
      )
      const saveButton = screen.getByRole('button', { name: 'ra.action.save' })
      expect(saveButton).toBeDisabled()
      fill('Mr. Mellow', '   ')
      expect(saveButton).toBeDisabled()
      fill('  ', 'Slow, soft songs.')
      expect(saveButton).toBeDisabled()
      fill('Mr. Mellow', 'Slow, soft songs.')
      expect(saveButton).toBeEnabled()
    })

    it('edits a DJ, starting from its name and description', async () => {
      serveDJs([...djs, mellow], {
        'PUT /dj/dj-1': (opts) => {
          const edited = { ...mellow, ...JSON.parse(opts.body) }
          serveDJs([...djs, edited])
          return Promise.resolve({ json: edited })
        },
      })
      renderPage()
      await screen.findByText('Mr. Mellow')

      fireEvent.click(
        screen.getByRole('button', { name: 'ra.action.edit Mr. Mellow' }),
      )
      expect(
        screen.getByLabelText(/^resources.station.dj.fields.name/),
      ).toHaveValue('Mr. Mellow')
      expect(
        screen.getByLabelText(/^resources.station.dj.fields.description/),
      ).toHaveValue('Slow, soft songs.')
      fill('Mr. Loud', 'Fast and loud.')
      save()

      expect(await screen.findByText('Mr. Loud')).toBeInTheDocument()
      const [[, opts]] = calls('PUT', '/dj/dj-1')
      expect(JSON.parse(opts.body)).toEqual({
        name: 'Mr. Loud',
        description: 'Fast and loud.',
      })
    })

    it('says why the server refused a DJ, keeping the editor open', async () => {
      serveDJs(djs, {
        'POST /dj': () => Promise.reject(httpError(400, 'invalidDJName')),
      })
      renderPage()
      await screen.findByText('Night Owl')
      fireEvent.click(
        screen.getByRole('button', { name: 'resources.station.dj.new' }),
      )
      fill('Mr. Mellow', 'Slow, soft songs.')
      save()

      await waitFor(() =>
        expect(notify).toHaveBeenCalledWith(
          'resources.station.errors.invalidDJName',
          { type: 'warning' },
        ),
      )
      expect(editor()).toBeInTheDocument()
    })

    it('deletes a DJ once the Listener confirms, ending its Station if on air', async () => {
      serveDJs([...djs, mellow], {
        'DELETE /dj/dj-1': () => {
          serveDJs(djs)
          return Promise.resolve({ status: 204 })
        },
      })
      const store = renderPage()
      fireEvent.click(
        await screen.findByRole('button', {
          name: 'resources.station.actions.start Mr. Mellow',
        }),
      )
      await waitFor(() => expect(store.getState().station.djId).toBe('dj-1'))

      fireEvent.click(
        screen.getByRole('button', { name: 'ra.action.delete Mr. Mellow' }),
      )
      expect(calls('DELETE', '/dj/dj-1')).toHaveLength(0)
      fireEvent.click(screen.getByRole('button', { name: 'ra.action.confirm' }))

      await waitFor(() =>
        expect(screen.queryByText('Mr. Mellow')).not.toBeInTheDocument(),
      )
      expect(calls('DELETE', '/dj/dj-1')).toHaveLength(1)
      expect(store.getState().station.djId).toBeNull()
    })
  })
})
