import React from 'react'
import { render, screen } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { Provider } from 'react-redux'
import { createStore } from 'redux'
import { TranslationProvider } from 'react-admin'
import polyglotI18nProvider from 'ra-i18n-polyglot'
import en from '../i18n/en.json'
import { SongTitleField } from './SongTitleField'

// t1 is stored on this device; nothing else is
vi.mock('../download/index', () => ({
  downloadsSupported: () => true,
  useTrackStored: (trackId) => trackId === 't1',
}))

const i18nProvider = polyglotI18nProvider(() => en, 'en')

const renderTitle = (record) =>
  render(
    <Provider store={createStore(() => ({ player: {} }))}>
      <TranslationProvider i18nProvider={i18nProvider}>
        <SongTitleField record={record} />
      </TranslationProvider>
    </Provider>,
  )

const rip = {
  source: 'YouTube',
  sourceUrl: 'https://www.youtube.com/watch?v=dQw4w9WgXcQ',
}

describe('<SongTitleField />', () => {
  it('marks a stored song as Downloaded, alongside its Ripped badge', () => {
    renderTitle({ id: 't1', title: 'Song', ...rip })

    expect(screen.getByRole('img', { name: 'Downloaded' })).toBeInTheDocument()
    expect(screen.getByText('Ripped')).toBeInTheDocument()
  })

  it('marks a Playlist Track by the Track it holds', () => {
    renderTitle({ id: '7', mediaFileId: 't1', title: 'Song' })

    expect(screen.getByRole('img', { name: 'Downloaded' })).toBeInTheDocument()
  })

  it('does not mark a song that is not stored', () => {
    renderTitle({ id: 't2', title: 'Song' })

    expect(
      screen.queryByRole('img', { name: 'Downloaded' }),
    ).not.toBeInTheDocument()
  })
})
