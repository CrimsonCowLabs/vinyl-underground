import { jwtDecode } from 'jwt-decode'
import { baseUrl } from '../../utils'
import { REST_URL } from '../../consts'
import {
  clientUniqueId,
  clientUniqueIdHeader,
} from '../../dataProvider/httpClient'

// The Files the admin can upload (#35), as the server checks them too.
export const MAX_UPLOAD_BYTES = 500 * 1024 * 1024
export const UPLOAD_EXTENSIONS = [
  'mp3',
  'm4a',
  'opus',
  'ogg',
  'flac',
  'aiff',
  'aif',
  'wav',
]
export const UPLOAD_ACCEPT = UPLOAD_EXTENSIONS.map((e) => `.${e}`).join(',')

const extension = (name) => {
  const dot = name.lastIndexOf('.')
  return dot < 0 ? '' : name.slice(dot + 1).toLowerCase()
}

// checkFile is why a File can't be uploaded (a translation key), or null. The server checks
// again, and sniffs the content too.
export const checkFile = (file) => {
  if (!UPLOAD_EXTENSIONS.includes(extension(file.name))) {
    return 'resources.rip.upload.badType'
  }
  if (file.size > MAX_UPLOAD_BYTES) {
    return 'resources.rip.upload.tooBig'
  }
  return null
}

// sendFile uploads one File with XMLHttpRequest, for its upload progress, into the upload job
// jobId (a new one when null). It resolves with the server's answer ({job, files}), and rejects
// with {status, body} or a network error.
export const sendFile = ({ file, jobId, onProgress, XHR = XMLHttpRequest }) =>
  new Promise((resolve, reject) => {
    const xhr = new XHR()
    const query = jobId ? `?job=${encodeURIComponent(jobId)}` : ''
    xhr.open('POST', baseUrl(`${REST_URL}/rip/upload${query}`))
    xhr.setRequestHeader('Accept', 'application/json')
    xhr.setRequestHeader(clientUniqueIdHeader, clientUniqueId)
    const token = localStorage.getItem('token')
    if (token) xhr.setRequestHeader('X-ND-Authorization', `Bearer ${token}`)
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && onProgress) onProgress(e.loaded / e.total)
    }
    xhr.onerror = () => reject(new Error('network error'))
    xhr.onload = () => {
      const refreshed = xhr.getResponseHeader('X-ND-Authorization')
      if (refreshed) {
        localStorage.setItem('token', refreshed)
        localStorage.setItem('userId', jwtDecode(refreshed).uid)
      }
      let body = null
      try {
        body = JSON.parse(xhr.responseText)
      } catch {
        body = null
      }
      if (xhr.status >= 200 && xhr.status < 300) resolve(body)
      else reject({ status: xhr.status, body })
    }
    const form = new FormData()
    form.append('file', file, file.name)
    xhr.send(form)
  })

// uploadAll uploads the Files one at a time, all into one upload job (one review card), and
// reports each File's state through onChange(states) as it goes: waiting, uploading (with its
// progress, 0 to 1), done or failed (with its error, a message or a translation key). Files
// checkFile refuses are never sent. It resolves with the states at the end.
export const uploadAll = async (files, { onChange, send = sendFile } = {}) => {
  const states = files.map((file) => {
    const error = checkFile(file)
    return {
      name: file.name,
      size: file.size,
      status: error ? 'failed' : 'waiting',
      progress: 0,
      error,
    }
  })
  const update = (i, change) => {
    states[i] = { ...states[i], ...change }
    if (onChange) onChange([...states])
  }
  if (onChange) onChange([...states])
  let jobId = null
  for (let i = 0; i < files.length; i++) {
    if (states[i].status === 'failed') continue
    update(i, { status: 'uploading' })
    try {
      const res = await send({
        file: files[i],
        jobId,
        onProgress: (progress) => update(i, { progress }),
      })
      jobId = res?.job?.id || jobId
      update(i, { status: 'done', progress: 1 })
    } catch (err) {
      const error =
        err?.body?.files?.[0]?.error ||
        err?.body?.error ||
        err?.message ||
        'resources.rip.upload.failed'
      update(i, { status: 'failed', error })
      if (err?.status === 404 && jobId) {
        // The card was discarded meanwhile: the next File starts a new one.
        jobId = null
      }
    }
  }
  return states
}
