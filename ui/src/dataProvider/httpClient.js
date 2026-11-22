import { fetchUtils } from 'react-admin'
import { v4 as uuidv4 } from 'uuid'
import { baseUrl } from '../utils'
import config from '../config'
import { jwtDecode } from 'jwt-decode'

const customAuthorizationHeader = 'X-ND-Authorization'
export const clientUniqueIdHeader = 'X-ND-Client-Unique-Id'
export const clientUniqueId = uuidv4()

// The headers every request sends: the client's ID, and the login token, when there is one
const authorize = (headers) => {
  headers.set(clientUniqueIdHeader, clientUniqueId)
  const token = localStorage.getItem('token')
  if (token) {
    headers.set(customAuthorizationHeader, `Bearer ${token}`)
  }
  return headers
}

// Keeps the token the server refreshed, if it did
const keepRefreshedToken = (response) => {
  const token = response.headers.get(customAuthorizationHeader)
  if (token) {
    const decoded = jwtDecode(token)
    localStorage.setItem('token', token)
    localStorage.setItem('userId', decoded.uid)
    // Avoid going to create admin dialog after logout/login without a refresh
    config.firstTime = false
  }
  return response
}

const httpClient = (url, options = {}) => {
  url = baseUrl(url)
  if (!options.headers) {
    options.headers = new Headers({ Accept: 'application/json' })
  }
  authorize(options.headers)
  return fetchUtils.fetchJson(url, options).then(keepRefreshedToken)
}

// fetchImage fetches an image with the login token in the Authorization header, where an <img>
// would have to carry it in its URL (the cover picker's previews). Show the Blob it resolves
// with through URL.createObjectURL, and revoke that when done. It rejects with { status } for a
// failed answer, and with an Error for one that isn't an image.
export const fetchImage = async (url, { signal } = {}) => {
  const response = await fetch(baseUrl(url), {
    headers: authorize(new Headers({ Accept: 'image/*' })),
    signal,
  })
  keepRefreshedToken(response)
  if (!response.ok) {
    throw { status: response.status }
  }
  const type = response.headers.get('Content-Type') || ''
  if (!type.startsWith('image/')) {
    throw new Error(`${type || 'the answer'} is not an image`)
  }
  return response.blob()
}

export default httpClient
