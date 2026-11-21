import { describe, it, expect, vi, beforeEach } from 'vitest'
import { checkFile, sendFile, uploadAll, MAX_UPLOAD_BYTES } from './uploadFiles'

const file = (name, size = 10) => ({ name, size })

describe('checkFile', () => {
  it.each(['a.mp3', 'a.M4A', 'a.opus', 'a.ogg', 'a.flac', 'a.aiff', 'a.wav'])(
    'takes %s',
    (name) => expect(checkFile(file(name))).toBeNull(),
  )

  it('refuses another type, and no extension', () => {
    expect(checkFile(file('a.wma'))).toBe('resources.rip.upload.badType')
    expect(checkFile(file('mp3'))).toBe('resources.rip.upload.badType')
  })

  it('refuses a File over 500 MB', () => {
    expect(checkFile(file('a.flac', MAX_UPLOAD_BYTES))).toBeNull()
    expect(checkFile(file('a.flac', MAX_UPLOAD_BYTES + 1))).toBe(
      'resources.rip.upload.tooBig',
    )
  })
})

describe('uploadAll', () => {
  it('sends the Files one at a time into one job, with each one’s progress', async () => {
    const calls = []
    const send = vi.fn(async ({ file, jobId, onProgress }) => {
      calls.push({ name: file.name, jobId })
      onProgress(0.5)
      return { job: { id: 'job-1' } }
    })
    const seen = []
    const states = await uploadAll([file('a.mp3'), file('b.flac')], {
      send,
      onChange: (s) => seen.push(s),
    })

    expect(calls).toEqual([
      { name: 'a.mp3', jobId: null },
      { name: 'b.flac', jobId: 'job-1' },
    ])
    expect(states.map((s) => s.status)).toEqual(['done', 'done'])
    expect(
      seen.some((s) => s[0].status === 'uploading' && s[0].progress === 0.5),
    ).toBe(true)
    expect(
      seen.some((s) => s[1].status === 'waiting' && s[0].status !== 'waiting'),
    ).toBe(true)
  })

  it('never sends a File it refuses, and shows the server’s reason for one it refused', async () => {
    const send = vi.fn(async ({ file }) => {
      if (file.name === 'fake.mp3') {
        throw {
          status: 422,
          body: {
            error: 'x',
            files: [
              {
                name: 'fake.mp3',
                error: "fake.mp3: its content isn't MP3 audio",
              },
            ],
          },
        }
      }
      return { job: { id: 'job-1' } }
    })
    const states = await uploadAll(
      [file('notes.txt'), file('fake.mp3'), file('ok.mp3')],
      { send },
    )

    expect(send).toHaveBeenCalledTimes(2)
    expect(states[0]).toMatchObject({
      status: 'failed',
      error: 'resources.rip.upload.badType',
    })
    expect(states[1]).toMatchObject({
      status: 'failed',
      error: "fake.mp3: its content isn't MP3 audio",
    })
    expect(states[2].status).toBe('done')
  })

  it('starts a new card when the batch’s card was discarded meanwhile', async () => {
    const jobIds = []
    let n = 0
    const send = vi.fn(async ({ jobId }) => {
      jobIds.push(jobId)
      n++
      if (n === 2) throw { status: 404, body: { error: 'gone' } }
      return { job: { id: `job-${n}` } }
    })
    await uploadAll([file('a.mp3'), file('b.mp3'), file('c.mp3')], { send })
    expect(jobIds).toEqual([null, 'job-1', null])
  })
})

describe('sendFile', () => {
  const made = []
  const last = () => made[made.length - 1]
  class FakeXHR {
    constructor() {
      made.push(this)
      this.headers = {}
      this.upload = {}
    }
    open(method, url) {
      this.method = method
      this.url = url
    }
    setRequestHeader(k, v) {
      this.headers[k] = v
    }
    getResponseHeader() {
      return null
    }
    send(body) {
      this.body = body
    }
  }

  beforeEach(() => localStorage.setItem('token', 'tok'))

  it('posts the File as multipart to the upload route, with the batch’s job and progress', async () => {
    const progress = []
    const done = sendFile({
      file: new File(['abc'], 'a.mp3'),
      jobId: 'job 1',
      onProgress: (p) => progress.push(p),
      XHR: FakeXHR,
    })
    expect(last().method).toBe('POST')
    expect(last().url).toMatch(/\/api\/rip\/upload\?job=job%201$/)
    expect(last().headers['X-ND-Authorization']).toBe('Bearer tok')
    expect(last().body.get('file').name).toBe('a.mp3')

    last().upload.onprogress({ lengthComputable: true, loaded: 1, total: 4 })
    last().status = 201
    last().responseText = '{"job":{"id":"job 1"},"files":[]}'
    last().onload()
    await expect(done).resolves.toEqual({ job: { id: 'job 1' }, files: [] })
    expect(progress).toEqual([0.25])
  })

  it('rejects with the status and the server’s answer', async () => {
    const done = sendFile({ file: new File(['abc'], 'a.mp3'), XHR: FakeXHR })
    last().status = 422
    last().responseText = '{"error":"no"}'
    last().onload()
    await expect(done).rejects.toEqual({ status: 422, body: { error: 'no' } })
  })
})
