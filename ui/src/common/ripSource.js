// ripSourceURL returns the web page a Rip came from, or null when the File isn't a Rip.
// Only the app's ripper (yt-dlp) writes an http(s) SOURCEURL; CD rippers and other taggers
// may write SOURCE (e.g. "CD"), but that alone doesn't make a File a Rip.
export const ripSourceURL = (record) => {
  const raw = record?.sourceUrl
  if (typeof raw !== 'string') {
    return null
  }
  try {
    const url = new URL(raw)
    if ((url.protocol === 'http:' || url.protocol === 'https:') && url.host) {
      return url
    }
  } catch {
    // not a URL
  }
  return null
}
