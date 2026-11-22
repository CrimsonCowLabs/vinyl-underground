import * as React from 'react'
import { TestContext } from 'ra-test'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { describe, afterEach, it, expect } from 'vitest'
import { SelectAccent } from './SelectAccent'
import { themeReducer } from '../reducers/themeReducer'
import { changeTheme } from '../actions'
import { AUTO_THEME_ID } from '../consts'

let store

// The accent picker on the settings page, with the per-device theme state a
// reload would bring back from localStorage.
const renderPicker = (savedTheme) =>
  render(
    <TestContext
      enableReducers
      customReducers={{ theme: themeReducer }}
      initialState={savedTheme && { theme: savedTheme }}
    >
      {({ store: testStore }) => {
        store = testStore
        return <SelectAccent />
      }}
    </TestContext>,
  )

const accent = (name) =>
  screen.getByRole('radio', {
    name: `menu.personal.options.accents.${name}`,
  })

describe('SelectAccent', () => {
  afterEach(cleanup)

  it('offers the eight accents and From album art, with Teal chosen on a new device', () => {
    renderPicker()
    expect(screen.getAllByRole('radio')).toHaveLength(9)
    expect(accent('teal')).toBeChecked()
    expect(accent('art')).not.toBeChecked()
  })

  it.each(['plum', 'art'])('stores choosing %s for this device', (name) => {
    renderPicker()
    fireEvent.click(accent(name))
    expect(accent(name)).toBeChecked()
    expect(accent('teal')).not.toBeChecked()
    expect(store.getState().theme.accent).toBe(name)
  })

  it('shows an accent chosen earlier on this device', () => {
    renderPicker({ choice: AUTO_THEME_ID, accent: 'olive' })
    expect(accent('olive')).toBeChecked()
  })

  it.each([
    AUTO_THEME_ID,
    'VinylUndergroundDarkTheme',
    'VinylUndergroundLightTheme',
  ])('is offered with %s', (id) => {
    renderPicker({ choice: id })
    expect(screen.getByRole('radiogroup')).toBeInTheDocument()
  })

  it.each(['NordTheme', 'DarkTheme', 'LightTheme'])(
    'is not offered with the inherited %s',
    (id) => {
      renderPicker({ choice: id })
      expect(screen.queryByRole('radiogroup')).not.toBeInTheDocument()
    },
  )

  it('goes and comes back as the colour theme changes, keeping the choice', () => {
    renderPicker({ choice: AUTO_THEME_ID, accent: 'rose' })
    act(() => {
      store.dispatch(changeTheme('NordTheme'))
    })
    expect(screen.queryByRole('radiogroup')).not.toBeInTheDocument()
    act(() => {
      store.dispatch(changeTheme('VinylUndergroundLightTheme'))
    })
    expect(accent('rose')).toBeChecked()
  })
})
