/* eslint-disable */

import { APP_SHELL_CACHE, createNavigationHandler } from './swNavigation'
import { createDownloadRoutes } from './download/swRoutes'
import { DOWNLOADS_CACHE } from './download/keys'

// documentation: https://developers.google.com/web/tools/workbox/modules/workbox-sw
importScripts('3rdparty/workbox/workbox-sw.js')

workbox.setConfig({
  modulePathPrefix: '3rdparty/workbox/',
  debug: false,
})

workbox.loadModule('workbox-core')
workbox.loadModule('workbox-strategies')
workbox.loadModule('workbox-routing')
workbox.loadModule('workbox-navigation-preload')
workbox.loadModule('workbox-precaching')

workbox.core.clientsClaim()
self.skipWaiting()

addEventListener('message', (event) => {
  if (event.data && event.data.type === 'SKIP_WAITING') {
    skipWaiting()
  }
})

const CACHE_NAME = 'offline-html'
// This assumes /offline.html is a URL for your self-contained
// (no external images or styles) offline page.
const FALLBACK_HTML_URL = './offline.html'
// Populate the cache with the offline HTML page when the
// service worker is installed.
self.addEventListener('install', async (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => cache.add(FALLBACK_HTML_URL)),
  )
})

const networkOnly = new workbox.strategies.NetworkOnly()
const fallbackToCachedHtml = () =>
  caches.match(FALLBACK_HTML_URL, { cacheName: CACHE_NAME })

// One copy of the app shell per device, whatever URL in scope loaded it
const appShell = {
  put: (response) =>
    caches
      .open(APP_SHELL_CACHE)
      .then((cache) => cache.put(self.registration.scope, response)),
  match: () =>
    caches.match(self.registration.scope, { cacheName: APP_SHELL_CACHE }),
}

const navigationHandler = createNavigationHandler(
  networkOnly,
  fallbackToCachedHtml,
  appShell,
)

// Downloads: Tracks and their cover art stored on the device for offline play
const downloadRoutes = createDownloadRoutes({
  match: (key) => caches.match(key, { cacheName: DOWNLOADS_CACHE }),
  fetch: (request) => fetch(request),
})
workbox.routing.registerRoute(downloadRoutes.isAudio, downloadRoutes.serveAudio)
workbox.routing.registerRoute(
  downloadRoutes.isCoverArt,
  downloadRoutes.serveCoverArt,
)

// self.__WB_MANIFEST is default injection point
workbox.precaching.precacheAndRoute(self.__WB_MANIFEST)

// Register this strategy to handle all navigations.
workbox.routing.registerRoute(
  new workbox.routing.NavigationRoute(navigationHandler),
)
