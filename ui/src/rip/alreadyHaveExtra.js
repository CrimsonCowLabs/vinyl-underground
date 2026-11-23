import React from 'react'
import { AlreadyHave } from './AlreadyHave'

// alreadyHaveExtra is the trackExtra(track) of the Rip list: the "already have" indicator with
// a Track's title, for an item of the job list and a Track of the review card alike, as both
// carry the item's alreadyHave matches.
export const alreadyHaveExtra = (track) =>
  React.createElement(AlreadyHave, { tracks: track.alreadyHave })
