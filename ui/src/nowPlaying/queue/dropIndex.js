// dropIndex is where a queued Track dragged from the index lands, dragged dy
// pixels (down is positive) in a list of count rows rowHeight high: one row
// further for each half row past.
export const dropIndex = (from, dy, rowHeight, count) =>
  Math.max(0, Math.min(count - 1, from + Math.round(dy / (rowHeight || 1))))
