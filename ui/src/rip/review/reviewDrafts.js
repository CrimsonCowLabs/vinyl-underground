import { httpClient } from '../../dataProvider'
import { jobUrl } from './reviewApi'

// A job's unsaved review edits, kept outside its review card. The Rip list remounts every
// expanded card each time it refreshes (react-admin 3 keys the list's content on its refresh
// counter), so edits held only in the card would be lost every few seconds. A remounted card
// takes its draft back from here instead of reloading the tags.
//
// A draft is { review, values }: the card the admin is editing against (so what counts as an edit
// stays the same, and proposals that arrive later still don't overwrite the edits) and the form's
// values. Drafts live in memory only: they survive refreshes, not a page reload.

const drafts = new Map()
// saves are the Saves still in flight, by job, so a card remounted meanwhile can wait for its own.
const saves = new Map()

export const getDraft = (jobId) => drafts.get(jobId) || null

export const setDraft = (jobId, draft) => {
  drafts.set(jobId, draft)
}

export const clearDraft = (jobId) => {
  drafts.delete(jobId)
}

// saveTags writes a job's edits (the PUT /api/rip/{jobId}/tags body) and resolves to the card as
// the Files now are. Once saved, the job's draft goes.
export const saveTags = (jobId, edits) => {
  const save = httpClient(`${jobUrl(jobId)}/tags`, {
    method: 'PUT',
    body: JSON.stringify(edits),
  })
    .then(({ json }) => {
      clearDraft(jobId)
      return json
    })
    .finally(() => {
      if (saves.get(jobId) === save) saves.delete(jobId)
    })
  saves.set(jobId, save)
  return save
}

// pendingSave is the job's Save still in flight, if any.
export const pendingSave = (jobId) => saves.get(jobId) || null

// clearAllDrafts is for tests, which reuse job ids.
export const clearAllDrafts = () => {
  drafts.clear()
  saves.clear()
}
