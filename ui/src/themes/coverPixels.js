// readCoverPixels fetches a cover and reads it, shrunk to 32×32, as RGBA
// pixels (a canvas's getImageData). It resolves to null for the server's
// placeholder (a missing cover), and rejects if the cover fails to load or
// can't be read.
//
// Pixels can only be read from an image that is same-origin or served with
// CORS. Covers come from the server's getCoverArt, same-origin (through the
// Vite dev server's proxy too), and a Download's covers are answered by the
// service worker from Cache Storage when offline, still same-origin. A cover
// on another origin is fetched with CORS, so if its host doesn't allow it,
// the fetch fails and the caller falls back.
//
// The server sends its placeholder, and only it, with Cache-Control:
// no-store (server/imghttp/headers.go), which is why this fetches rather
// than loading an <img>: an <img> can't see the headers.
const SIZE = 32

const isPlaceholder = (res) =>
  /no-store/.test(res.headers.get('Cache-Control') || '')

export const readCoverPixels = async (url) => {
  const res = await fetch(url)
  if (!res.ok) throw new Error(`cover failed to load (${res.status})`)
  if (isPlaceholder(res)) return null
  const bitmap = await createImageBitmap(await res.blob())
  const canvas = document.createElement('canvas')
  canvas.width = SIZE
  canvas.height = SIZE
  const ctx = canvas.getContext('2d')
  ctx.drawImage(bitmap, 0, 0, SIZE, SIZE)
  bitmap.close?.()
  return ctx.getImageData(0, 0, SIZE, SIZE).data
}
