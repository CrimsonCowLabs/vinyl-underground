import React from 'react'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, it, expect, vi } from 'vitest'
import DownloadsPage from './DownloadsPage'

vi.mock('react-admin', () => ({
  Title: ({ title }) => <h1>{title}</h1>,
  useTranslate: () => (key) => key,
}))

vi.mock('./ShuffleDownloadsButton', () => ({
  ShuffleDownloadsButton: () => <button>shuffle all Downloads</button>,
}))

vi.mock('../download', () => ({
  downloadsSupported: () => true,
  useDownloadUsage: () => ({
    total: 150e6,
    downloads: [
      { id: 'al1', kind: 'album', name: 'Blue', size: 90e6 },
      { id: 'pl1', kind: 'playlist', name: 'Road trip', size: 60e6 },
    ],
  }),
}))

describe('<DownloadsPage />, the Downloads tab', () => {
  it('offers to shuffle the Downloads and opens each one', () => {
    render(
      <MemoryRouter>
        <DownloadsPage />
      </MemoryRouter>,
    )
    expect(
      screen.getByRole('heading', { name: /download.storage.title/ }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'shuffle all Downloads' }),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Blue/ })).toHaveAttribute(
      'href',
      '/album/al1/show',
    )
    expect(screen.getByRole('button', { name: /Road trip/ })).toHaveAttribute(
      'href',
      '/playlist/pl1/show',
    )
  })
})
