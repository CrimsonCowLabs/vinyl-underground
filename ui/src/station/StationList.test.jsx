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

// The Library's genres, and the ones the server matches from a DJ's name and
// description: those whose name is in them.
const libraryGenres = ['House', 'Rock', 'Techno']
const matchGenres = (url) => {
  const q = new URLSearchParams(url.split('?')[1])
  const text = `${q.get('name')} ${q.get('description')}`.toLowerCase()
  return libraryGenres.filter((g) => text.includes(g.toLowerCase()))
}

// serveDJs answers the DJ list with the DJs given (built-in ones by default),
// the Library's genres, opening, saving, deleting and starting a Station with
// what the server would. Opening a DJ with no genres, nor picked by hand,
// matches them.
const serveDJs = (list = djs, overrides = {}) =>
  httpClient.mockImplementation((url, opts = {}) => {
    const method = opts.method || 'GET'
    const key = `${method} ${url.replace(/^.*\/station/, '')}`
    if (overrides[key]) return overrides[key](opts)
    if (key === 'GET /dj') return Promise.resolve({ json: list })
    const opened = list.find((dj) => key === `GET /dj/${dj.id}`)
    if (opened) {
      const genres =
        opened.genres?.length || opened.genresEdited
          ? opened.genres
          : matchGenres(
              `?${new URLSearchParams({ name: opened.name, description: opened.description })}`,
            )
      return Promise.resolve({ json: { ...opened, genres } })
    }
    if (key === 'GET /genres') return Promise.resolve({ json: libraryGenres })
    if (key.startsWith('GET /genres/match?')) {
      return Promise.resolve({ json: matchGenres(url) })
    }
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

  it('says the DJ is picking the first Tracks until the Station starts', async () => {
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
    expect(store.getState().station.picking).toMatchObject({
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

  it("starts the Station started last, whatever an earlier start's answer", async () => {
    const answers = []
    serveDJs(djs, {
      'POST /next': () =>
        new Promise((resolve, reject) => answers.push({ resolve, reject })),
    })
    const store = renderPage()
    await screen.findByText('Night Owl')
    const startDJ = (name) =>
      fireEvent.click(
        screen.getByRole('button', {
          name: `resources.station.actions.start ${name}`,
        }),
      )

    startDJ('Night Owl')
    await screen.findByText('resources.station.picking')
    act(() => {
      store.dispatch(playTracks({ x: { id: 'x', title: 'X' } }))
    })
    await waitFor(() =>
      expect(
        screen.getByRole('button', {
          name: 'resources.station.actions.start Sunny',
        }),
      ).toBeEnabled(),
    )
    startDJ('Sunny')
    await waitFor(() => expect(answers).toHaveLength(2))

    answers[0].reject(httpError(504, 'timeout'))
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(store.getState().station.picking?.djId).toBe('sunny')
    answers[1].resolve({ json: { tracks: firstTracks } })

    await waitFor(() => expect(store.getState().station.djId).toBe('sunny'))
    expect(notify).not.toHaveBeenCalled()
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

    describe('setup step, with an LLM', () => {
      // pending returns a promise and what settles it, for a slow server.
      const pending = () => {
        let settle
        const promise = new Promise((resolve, reject) => {
          settle = { resolve, reject }
        })
        return { promise, settle }
      }

      beforeEach(() => {
        config.enableStationThemes = true
      })

      const openNew = async () => {
        renderPage()
        await screen.findByText('Night Owl')
        fireEvent.click(
          screen.getByRole('button', { name: 'resources.station.dj.new' }),
        )
        fill('Mr. Mellow', 'Slow, soft songs.')
      }

      // serveSetup has saving the DJ and its setup step wait for the test.
      const serveSetup = () => {
        const saving = pending()
        const setup = pending()
        serveDJs(djs, {
          'POST /dj': () => saving.promise,
          'GET /dj/dj-1/setup': () => setup.promise,
        })
        return { saving: saving.settle, setup: setup.settle }
      }

      it('shows its progress while the genres are read and the first hour picked', async () => {
        const server = serveSetup()
        await openNew()
        save()

        expect(
          await screen.findByText('resources.station.dj.setup.readingGenres'),
        ).toBeInTheDocument()
        serveDJs([...djs, mellow], {
          'GET /dj/dj-1/setup': () => new Promise(() => {}),
        })
        server.saving.resolve({ json: mellow })
        expect(
          await screen.findByText('resources.station.dj.setup.picking'),
        ).toBeInTheDocument()
        expect(
          screen.queryByText('resources.station.dj.setup.readingGenres'),
        ).not.toBeInTheDocument()
        expect(calls('GET', '/dj/dj-1/setup')).toHaveLength(1)
        expect(editor()).toBeInTheDocument()
      })

      it("goes straight to picking when the genres were picked by hand, as there's none to read", async () => {
        serveSetup()
        await openNew()
        fill('Mr. Mellow', 'Slow house.')
        fireEvent.blur(
          screen.getByLabelText(/^resources.station.dj.fields.description/),
        )
        fireEvent.click(
          await screen.findByRole('button', { name: 'ra.action.remove House' }),
        )
        save()

        expect(
          await screen.findByText('resources.station.dj.setup.picking'),
        ).toBeInTheDocument()
        expect(
          screen.queryByText('resources.station.dj.setup.readingGenres'),
        ).not.toBeInTheDocument()
      })

      it('closes once the DJ is ready, listing it', async () => {
        serveDJs(djs, {
          'POST /dj': () => {
            serveDJs([...djs, mellow], {
              'GET /dj/dj-1/setup': () =>
                Promise.resolve({ json: { status: 'ready' } }),
            })
            return Promise.resolve({ json: mellow })
          },
        })
        await openNew()
        save()

        await waitFor(() =>
          expect(screen.queryByRole('dialog')).not.toBeInTheDocument(),
        )
        expect(screen.getByText('Mr. Mellow')).toBeInTheDocument()
        expect(calls('GET', '/dj/dj-1/setup')).toHaveLength(1)
        expect(
          screen.queryByText('resources.station.dj.setup.settingUp'),
        ).not.toBeInTheDocument()
      })

      it('says the DJ is still being set up when the setup step ran out', async () => {
        const server = serveSetup()
        await openNew()
        save()
        server.saving.resolve({ json: mellow })
        server.setup.resolve({ json: { status: 'settingUp' } })

        expect(
          await screen.findByText('resources.station.dj.setup.settingUp'),
        ).toBeInTheDocument()
        expect(editor()).toBeInTheDocument()
        serveDJs([...djs, mellow])
        fireEvent.click(
          screen.getByRole('button', {
            name: 'resources.station.dj.setup.done',
          }),
        )
        await waitFor(() =>
          expect(screen.queryByRole('dialog')).not.toBeInTheDocument(),
        )
        expect(await screen.findByText('Mr. Mellow')).toBeInTheDocument()
      })

      it('says the DJ is still being set up when asking after it failed', async () => {
        const server = serveSetup()
        await openNew()
        save()
        server.saving.resolve({ json: mellow })
        server.setup.reject(httpError(500, 'internal'))

        expect(
          await screen.findByText('resources.station.dj.setup.settingUp'),
        ).toBeInTheDocument()
      })

      it('sets up an edited DJ too', async () => {
        serveDJs([...djs, mellow], {
          'PUT /dj/dj-1': () => Promise.resolve({ json: mellow }),
          'GET /dj/dj-1/setup': () =>
            Promise.resolve({ json: { status: 'settingUp' } }),
        })
        renderPage()
        await screen.findByText('Mr. Mellow')
        fireEvent.click(
          screen.getByRole('button', { name: 'ra.action.edit Mr. Mellow' }),
        )
        save()

        expect(
          await screen.findByText('resources.station.dj.setup.settingUp'),
        ).toBeInTheDocument()
        expect(calls('GET', '/dj/dj-1/setup')).toHaveLength(1)
      })

      it('has no setup step without an LLM', async () => {
        config.enableStationThemes = false
        serveDJs(djs, {
          'POST /dj': () => {
            serveDJs([...djs, mellow])
            return Promise.resolve({ json: mellow })
          },
        })
        await openNew()
        save()

        await waitFor(() =>
          expect(screen.queryByRole('dialog')).not.toBeInTheDocument(),
        )
        expect(calls('GET', '/dj/dj-1/setup')).toHaveLength(0)
      })
    })

    describe('genres', () => {
      const description = () =>
        screen.getByLabelText(/^resources.station.dj.fields.description/)
      const genreTags = () =>
        screen.queryAllByTestId('dj-genre').map((chip) => chip.textContent)
      const matchCalls = () =>
        httpClient.mock.calls.filter(([url]) =>
          url.includes('/station/genres/match?'),
        )
      // resuggestButton waits until the DJ opened, so the button is enabled.
      const resuggestButton = async () => {
        const button = screen.getByRole('button', {
          name: 'resources.station.dj.resuggest',
        })
        await waitFor(() => expect(button).toBeEnabled())
        return button
      }
      const openNew = async () => {
        renderPage()
        await screen.findByText('Night Owl')
        fireEvent.click(
          screen.getByRole('button', { name: 'resources.station.dj.new' }),
        )
      }
      const techno = {
        ...mellow,
        name: 'Tina',
        description: 'Techno and house all night.',
        genres: ['Techno', 'House'],
      }

      it('shows the genres matched from the name and description, with a tip', async () => {
        await openNew()
        expect(
          screen.getByText('resources.station.dj.fields.genresTip'),
        ).toBeInTheDocument()
        fill('Tina', 'Pounding techno all night.')
        fireEvent.blur(description())

        await waitFor(() => expect(genreTags()).toEqual(['Techno']))
        const [[url]] = matchCalls()
        expect(url).toContain('name=Tina')
        expect(url).toContain('description=Pounding+techno+all+night.')
        expect(
          screen.queryByText('resources.station.dj.noGenresWarning'),
        ).not.toBeInTheDocument()

        save()
        await waitFor(() => expect(calls('POST', '/dj')).toHaveLength(1))
        // Left to the server, which matches them the same way.
        expect(JSON.parse(calls('POST', '/dj')[0][1].body)).toEqual({
          name: 'Tina',
          description: 'Pounding techno all night.',
        })
      })

      it('warns when no genre matched and no LLM is configured', async () => {
        await openNew()
        fill('Mr. Mellow', 'Slow, soft songs.')
        fireEvent.blur(description())
        expect(
          await screen.findByText('resources.station.dj.noGenresWarning'),
        ).toBeInTheDocument()
      })

      it('only warns once matching found nothing, not while typing', async () => {
        await openNew()
        fill('Mr. Mellow', 'Slow, soft songs.')
        expect(
          screen.queryByText('resources.station.dj.noGenresWarning'),
        ).not.toBeInTheDocument()
      })

      it("doesn't warn when the Listener removed every genre", async () => {
        serveDJs([...djs, techno])
        renderPage()
        fireEvent.click(
          await screen.findByRole('button', { name: 'ra.action.edit Tina' }),
        )
        fireEvent.click(
          screen.getByRole('button', { name: 'ra.action.remove House' }),
        )
        fireEvent.click(
          screen.getByRole('button', { name: 'ra.action.remove Techno' }),
        )
        expect(genreTags()).toEqual([])
        expect(
          screen.queryByText('resources.station.dj.noGenresWarning'),
        ).not.toBeInTheDocument()
      })

      it('matches them on opening a DJ saved before it had any', async () => {
        const old = { ...techno, genres: undefined }
        serveDJs([...djs, old])
        renderPage()
        fireEvent.click(
          await screen.findByRole('button', { name: 'ra.action.edit Tina' }),
        )
        await waitFor(() => expect(genreTags()).toEqual(['House', 'Techno']))
      })

      it('show the genres the server maps on opening a DJ, while suggesting them', async () => {
        let answer
        serveDJs([...djs, techno], {
          'GET /dj/dj-1': () =>
            new Promise((resolve) => {
              answer = resolve
            }),
        })
        renderPage()
        fireEvent.click(
          await screen.findByRole('button', { name: 'ra.action.edit Tina' }),
        )
        expect(genreTags()).toEqual(['Techno', 'House'])
        expect(
          screen.getByText('resources.station.dj.suggestingGenres'),
        ).toBeInTheDocument()

        answer({ json: { ...techno, genres: ['Rock', 'House'] } })
        await waitFor(() => expect(genreTags()).toEqual(['Rock', 'House']))
        expect(
          screen.queryByText('resources.station.dj.suggestingGenres'),
        ).not.toBeInTheDocument()
      })

      it('keep the genres picked by hand while the DJ was opening', async () => {
        let answer
        serveDJs([...djs, techno], {
          'GET /dj/dj-1': () =>
            new Promise((resolve) => {
              answer = resolve
            }),
        })
        renderPage()
        fireEvent.click(
          await screen.findByRole('button', { name: 'ra.action.edit Tina' }),
        )
        fireEvent.click(
          screen.getByRole('button', { name: 'ra.action.remove House' }),
        )
        answer({ json: { ...techno, genres: ['Rock', 'House'] } })
        await waitFor(() =>
          expect(
            screen.queryByText('resources.station.dj.suggestingGenres'),
          ).not.toBeInTheDocument(),
        )
        expect(genreTags()).toEqual(['Techno'])
      })

      it('are suggested again from the description on request, replacing the ones picked by hand', async () => {
        serveDJs([...djs, { ...techno, genresEdited: true }], {
          'PUT /dj/dj-1': (opts) =>
            Promise.resolve({ json: { ...techno, ...JSON.parse(opts.body) } }),
        })
        renderPage()
        fireEvent.click(
          await screen.findByRole('button', { name: 'ra.action.edit Tina' }),
        )
        fill('Tina', 'Rock now.')
        fireEvent.blur(description())
        expect(genreTags()).toEqual(['Techno', 'House'])

        fireEvent.click(await resuggestButton())
        await waitFor(() => expect(genreTags()).toEqual(['Rock']))
        const [[url]] = matchCalls()
        expect(url).toContain('description=Rock+now.')

        save()
        await waitFor(() => expect(calls('PUT', '/dj/dj-1')).toHaveLength(1))
        expect(JSON.parse(calls('PUT', '/dj/dj-1')[0][1].body)).toEqual({
          name: 'Tina',
          description: 'Rock now.',
          resuggest: true,
        })
      })

      it('follow the description again once suggested again', async () => {
        serveDJs([...djs, { ...techno, genresEdited: true }])
        renderPage()
        fireEvent.click(
          await screen.findByRole('button', { name: 'ra.action.edit Tina' }),
        )
        fireEvent.click(await resuggestButton())
        await waitFor(() => expect(genreTags()).toEqual(['House', 'Techno']))
        fill('Tina', 'Rock now.')
        fireEvent.blur(description())
        await waitFor(() => expect(genreTags()).toEqual(['Rock']))
      })

      it("doesn't warn when an LLM is configured", async () => {
        config.enableStationThemes = true
        await openNew()
        fill('Mr. Mellow', 'Slow, soft songs.')
        fireEvent.blur(description())
        await waitFor(() => expect(matchCalls()).toHaveLength(1))
        expect(
          screen.queryByText('resources.station.dj.noGenresWarning'),
        ).not.toBeInTheDocument()
      })

      it("can be removed and added from the Library's, saved as picked by hand", async () => {
        serveDJs([...djs, techno], {
          'PUT /dj/dj-1': (opts) =>
            Promise.resolve({ json: { ...techno, ...JSON.parse(opts.body) } }),
        })
        renderPage()
        fireEvent.click(
          await screen.findByRole('button', { name: 'ra.action.edit Tina' }),
        )
        expect(genreTags()).toEqual(['Techno', 'House'])

        fireEvent.click(
          screen.getByRole('button', { name: 'ra.action.remove House' }),
        )
        const add = screen.getByLabelText(
          'resources.station.dj.fields.addGenre',
        )
        fireEvent.change(add, { target: { value: 'ro' } })
        fireEvent.click(await screen.findByRole('option', { name: 'Rock' }))
        expect(genreTags()).toEqual(['Techno', 'Rock'])

        // Picked by hand, so the description no longer changes them.
        fill('Tina', 'Only house now.')
        fireEvent.blur(description())
        expect(matchCalls()).toHaveLength(0)
        expect(genreTags()).toEqual(['Techno', 'Rock'])

        save()
        await waitFor(() => expect(calls('PUT', '/dj/dj-1')).toHaveLength(1))
        expect(JSON.parse(calls('PUT', '/dj/dj-1')[0][1].body)).toEqual({
          name: 'Tina',
          description: 'Only house now.',
          genres: ['Techno', 'Rock'],
        })
      })

      it('stay as picked by hand when the description changes', async () => {
        serveDJs([...djs, { ...techno, genresEdited: true }])
        renderPage()
        fireEvent.click(
          await screen.findByRole('button', { name: 'ra.action.edit Tina' }),
        )
        fill('Tina', 'Rock now.')
        fireEvent.blur(description())
        expect(matchCalls()).toHaveLength(0)
        expect(genreTags()).toEqual(['Techno', 'House'])

        save()
        await waitFor(() => expect(calls('PUT', '/dj/dj-1')).toHaveLength(1))
        expect(JSON.parse(calls('PUT', '/dj/dj-1')[0][1].body)).toEqual({
          name: 'Tina',
          description: 'Rock now.',
        })
      })
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
