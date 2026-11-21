import React from 'react'
import { render, screen } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { Provider } from 'react-redux'
import { createStore } from 'redux'
import { TranslationProvider } from 'react-admin'
import polyglotI18nProvider from 'ra-i18n-polyglot'
import en from '../i18n/en.json'
import { SongSimpleList } from './SongSimpleList'

// t1 is stored on this device; nothing else is
vi.mock('../download/index', () => ({
  downloadsSupported: () => true,
  useTrackStored: (trackId) => trackId === 't1',
}))

// The row's controls need the player and the server; only the title matters here
vi.mock('./index', () => ({
  DurationField: () => null,
  RatingField: () => null,
  SongContextMenu: () => null,
}))

const i18nProvider = polyglotI18nProvider(() => en, 'en')

describe('<SongSimpleList />', () => {
  it('marks the stored songs of the mobile song list as Downloaded', () => {
    render(
      <Provider store={createStore(() => ({}))}>
        <TranslationProvider i18nProvider={i18nProvider}>
          <SongSimpleList
            ids={['t1', 't2']}
            data={{
              t1: { id: 't1', title: 'Stored' },
              t2: { id: 't2', title: 'Not stored' },
            }}
            total={2}
          />
        </TranslationProvider>
      </Provider>,
    )

    expect(screen.getAllByRole('img', { name: 'Downloaded' })).toHaveLength(1)
    expect(
      screen.getByText('Stored').querySelector('[aria-label="Downloaded"]'),
    ).toBeInTheDocument()
  })
})
