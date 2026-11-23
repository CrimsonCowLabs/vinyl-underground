import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'

// A bulk edit's Tracks, the field it sets and the values it sets it to, as the server takes it:
// the selected Tracks by ID, or, for all matching, the list's filter, which the server resolves
// itself (so a filter matching the whole Library ships no IDs). Clearing sends no values.
export const bulkEditRequest = ({ selection, field, values, clear }) => {
  const target = selection.filter
    ? { filter: selection.filter }
    : { ids: selection.ids }
  const vs = clear
    ? []
    : (values || []).map((v) => v.trim()).filter((v) => v !== '')
  return { ...target, field, values: vs }
}

// matchingFilter is the list's filter for "all matching", without the inputs left empty.
export const matchingFilter = (filterValues) =>
  Object.fromEntries(
    Object.entries(filterValues || {}).filter(
      ([, v]) =>
        v !== undefined &&
        v !== null &&
        v !== '' &&
        !(Array.isArray(v) && v.length === 0),
    ),
  )

// The fields a bulk edit can set: [{ key, type, multi }].
export const getBulkFields = () =>
  httpClient(`${REST_URL}/libraryManager/fields`).then(({ json }) => json)

// What the edit would do: { total, changes, unchanged, refused: { code: n }, rows: [{ id,
// title, artist, album, path, old, new, status }] }, with the rows from start to end.
export const previewBulkEdit = (request, start = 0, end = 100) =>
  httpClient(`${REST_URL}/libraryManager/preview?_start=${start}&_end=${end}`, {
    method: 'POST',
    body: JSON.stringify(request),
  }).then(({ json }) => json)

// Runs the edit as one edit job: { job, background }, background while the job runs on after
// the server stopped waiting for it (the Activity panel follows it).
export const runBulkEdit = (request) =>
  httpClient(`${REST_URL}/libraryManager/edit`, {
    method: 'POST',
    body: JSON.stringify(request),
  }).then(({ status, json }) => ({ job: json, background: status === 202 }))
