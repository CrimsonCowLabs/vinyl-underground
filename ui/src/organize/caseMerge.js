import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'

// The case merge through Organize (#169), for every library the app can write.

// The next phase's plan: { token, phase ('rename', 'merge' or ''), counts: { op: n }, clashes:
// [{ libraryId, parent, names }], steps: [{ op, path, newPath, keep }], decide: [{ path,
// newPath }], totalClashes, totalSteps, totalDecide }, each list from start to end.
export const planCaseMerge = (start = 0, end = 100) =>
  httpClient(`${REST_URL}/organize/case/plan?_start=${start}&_end=${end}`, {
    method: 'POST',
    body: '{}',
  }).then(({ json }) => json)

// Runs the plan reviewed (its token) as one edit job: { job, background }, as Organize's run.
export const runCaseMerge = (token) =>
  httpClient(`${REST_URL}/organize/case/run`, {
    method: 'POST',
    body: JSON.stringify({ token }),
  }).then(({ status, json }) => ({ job: json, background: status === 202 }))

// The kinds of step a plan counts, in the order the summary lists them.
export const stepOps = ['rename', 'move', 'rmdir', 'quarantine']
