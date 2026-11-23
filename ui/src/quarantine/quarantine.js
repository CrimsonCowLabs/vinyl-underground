import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'

// Quarantine from the app (#168). Each write answers { job, background }: background while the
// job runs on after the server stopped waiting for it (the Activity panel follows it).

const post = (path, body) =>
  httpClient(`${REST_URL}/quarantine/${path}`, {
    method: 'POST',
    body: JSON.stringify(body),
  }).then(({ status, json }) => ({ job: json, background: status === 202 }))

// What is in Quarantine: { count, size, files: [{ libraryId, library, path, size, original,
// entryId, trackId, title, artist, album }] }.
export const getQuarantine = () =>
  httpClient(`${REST_URL}/quarantine`).then(({ json }) => json)

// Sends Tracks to Quarantine, as one job: { ids }, { albumId } or the Library manager's
// { filter }.
export const sendToQuarantine = (selection) => post('send', selection)

// Restores one File from Quarantine to where it came from.
export const restoreFromQuarantine = ({ libraryId, path }) =>
  post('restore', { libraryId, path })

// Empty Quarantine, for the Files the admin confirmed: as many, and as large, as shown.
export const emptyQuarantine = ({ count, size }) =>
  post('empty', { count, size })

// skippedFiles counts the Files a finished job skipped.
export const skippedFiles = (job) => job?.skipped || 0

const units = ['bytes', 'KB', 'MB', 'GB', 'TB']

// formatSize is a size in bytes as people read it: "1.5 GB".
export const formatSize = (bytes) => {
  let n = bytes || 0
  let u = 0
  while (n >= 1024 && u < units.length - 1) {
    n /= 1024
    u++
  }
  return u === 0 ? `${n} ${units[0]}` : `${n.toFixed(1)} ${units[u]}`
}
