// The Edit dialog's values: every field is a list (a multi-valued field such as genres can hold
// several values, and a File may hold several of any tag). No values means the tag isn't there.

// editableValues gives each field the File's values, or one blank input to fill in.
export const editableValues = (fields, values) =>
  Object.fromEntries(
    fields.map((f) => {
      const vs = values?.[f.key] || []
      return [f.key, vs.length ? [...vs] : ['']]
    }),
  )

const normalize = (f, vs) =>
  (vs || [])
    .map((v) => (f.type === 'longtext' ? v.replace(/\s+$/, '') : v.trim()))
    .filter((v) => v !== '')

const same = (a, b) => a.length === b.length && a.every((v, i) => v === b[i])

// changedValues are the fields whose values the admin changed, as the server takes them: only
// those are written into the File.
export const changedValues = (fields, original, edited) => {
  const res = {}
  for (const f of fields) {
    const before = normalize(f, original?.[f.key])
    const after = normalize(f, edited?.[f.key])
    if (!same(before, after)) res[f.key] = after
  }
  return res
}
