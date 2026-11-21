// Lives outside sw.js so it can be unit tested: sw.js only loads inside a
// service worker, where workbox arrives via importScripts.
//
// index.html is rendered per Listener by the server, so it is never
// precached. Instead, `shell` (optional: { put(response), match() }) keeps the
// last copy that loaded, and it opens the app when the network is gone so
// Downloads can still be played.
export const APP_SHELL_CACHE = 'app-shell'

export const createNavigationHandler =
  (networkOnly, offlineFallback, shell) => async (params) => {
    const fallback = async () => (await shell?.match()) || offlineFallback()
    try {
      // Attempt a network request.
      const response = await networkOnly.handle(params)
      // A 5xx reaches us as a normal response, but carries no usable app
      if (response.status >= 500) return fallback()
      if (shell && response.status === 200) {
        // Not awaited: the page shouldn't wait for the copy to be written
        shell.put(response.clone()).catch(() => {})
      }
      return response
    } catch (error) {
      // If it fails, return the stored app shell or the offline page.
      return fallback()
    }
  }
