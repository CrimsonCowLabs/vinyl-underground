// The error codes the Station endpoints answer with ({"error": code}) that
// have their own message.
const KNOWN = [
  'noTracks',
  'unknownDJ',
  'builtInDJ',
  'invalidTheme',
  'invalidDJName',
  'invalidDJDescription',
]

// stationErrorCode is the code a failed Station request answered with.
export const stationErrorCode = (err) => err?.body?.error

// stationErrorKey is the translation key telling the Listener why a Station
// request failed.
export const stationErrorKey = (err) => {
  const code = stationErrorCode(err)
  return KNOWN.includes(code)
    ? `resources.station.errors.${code}`
    : 'resources.station.errors.unavailable'
}
