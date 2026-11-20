import React from 'react'
import { render, screen } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import PlaylistCreate from './PlaylistCreate'

vi.mock('react-admin', () => ({
  Create: ({ children }) => <div>{children}</div>,
  SimpleForm: ({ children }) => <form>{children}</form>,
  TextInput: ({ source }) => <input name={source} />,
  BooleanInput: ({ source, initialValue }) => (
    <input
      type="checkbox"
      name={source}
      aria-label={source}
      defaultChecked={initialValue}
    />
  ),
  required: () => () => undefined,
  useTranslate: () => (key) => key,
  useRefresh: () => vi.fn(),
  useNotify: () => vi.fn(),
  useRedirect: () => vi.fn(),
}))

vi.mock('../common', () => ({
  Title: () => null,
}))

describe('<PlaylistCreate />', () => {
  it('creates private Playlists by default', () => {
    render(<PlaylistCreate basePath="/playlist" />)
    expect(screen.getByLabelText('public').checked).toBe(false)
  })
})
