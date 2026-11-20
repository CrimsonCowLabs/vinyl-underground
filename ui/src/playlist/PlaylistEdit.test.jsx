import React from 'react'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route } from 'react-router-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { PlaylistEditForm } from './PlaylistEdit'

vi.mock('react-admin', () => ({
  Edit: ({ children }) => <div>{children}</div>,
  SimpleForm: ({ children }) => <form>{children}</form>,
  FormDataConsumer: () => null,
  TextInput: ({ source }) => <input aria-label={source} />,
  TextField: () => null,
  BooleanInput: ({ source }) => <input aria-label={source} />,
  ReferenceInput: ({ children }) => <div>{children}</div>,
  SelectInput: () => null,
  required: () => () => undefined,
  useTranslate: () => (key) => key,
  usePermissions: () => ({ permissions: 'admin' }),
}))

vi.mock('../common', () => ({
  Title: () => null,
  isWritable: (ownerId) => localStorage.getItem('userId') === ownerId,
}))

const renderAt = (record) =>
  render(
    <MemoryRouter initialEntries={[`/playlist/${record.id}`]}>
      <Route
        path="/playlist/:id/show"
        render={() => <div>playlist page</div>}
      />
      <Route
        exact
        path="/playlist/:id"
        render={() => <PlaylistEditForm record={record} />}
      />
    </MemoryRouter>,
  )

describe('<PlaylistEditForm />', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('userId', 'admin-1')
  })

  it("sends another Listener's Playlist back to its read-only page", () => {
    renderAt({ id: 'pls-shared', ownerId: 'owner-1', public: true })
    expect(screen.getByText('playlist page')).toBeTruthy()
    expect(screen.queryByLabelText('name')).toBeNull()
  })

  it('lets the owner edit their Playlist', () => {
    renderAt({ id: 'pls-mine', ownerId: 'admin-1' })
    expect(screen.getByLabelText('name')).toBeTruthy()
  })
})
