import * as React from 'react'
import { TestContext } from 'ra-test'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { describe, afterEach, it, expect } from 'vitest'
import { AudioEffectsToggle } from './AudioEffectsToggle'
import { settingsReducer } from '../reducers/settingsReducer'

let store

// The switch on the settings page, with the per-device settings a reload
// would bring back from localStorage.
const renderToggle = (savedSettings) =>
  render(
    <TestContext
      enableReducers
      customReducers={{ settings: settingsReducer }}
      initialState={savedSettings && { settings: savedSettings }}
    >
      {({ store: testStore }) => {
        store = testStore
        return <AudioEffectsToggle />
      }}
    </TestContext>,
  )

describe('AudioEffectsToggle', () => {
  afterEach(cleanup)

  it('is off by default, with the locked-screen warning', () => {
    renderToggle()
    expect(screen.getByRole('checkbox')).not.toBeChecked()
    expect(
      screen.getByText('menu.personal.options.audioEffects.help'),
    ).toBeInTheDocument()
  })

  it('stores the choice in the per-device settings', () => {
    renderToggle()
    fireEvent.click(screen.getByRole('checkbox'))
    expect(screen.getByRole('checkbox')).toBeChecked()
    expect(store.getState().settings.audioEffects).toBe(true)

    fireEvent.click(screen.getByRole('checkbox'))
    expect(store.getState().settings.audioEffects).toBe(false)
  })

  it('shows a choice saved earlier on this device', () => {
    renderToggle({ audioEffects: true })
    expect(screen.getByRole('checkbox')).toBeChecked()
  })
})
