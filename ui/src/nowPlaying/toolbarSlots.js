// The Now Playing toolbar's slots, in the order they show. Each belongs to a
// feature built in its own issue: Lyrics and Queue (#92), EQ (#95), Visuals
// (#96), Sleep timer and Speed (#93). A slot shows only once its feature has
// registered a tool for it, so the toolbar holds just the features built so
// far, and is left out while none are.
export const TOOLBAR_SLOTS = [
  'lyrics',
  'queue',
  'eq',
  'visuals',
  'sleep',
  'speed',
]

const tools = {}

// registerNowPlayingTool fills a slot with a tool: a component, usually a
// NowPlayingToolButton, given the view's { onNavigate } so it can leave the
// view the way its links do. A feature registers at module load, before Now
// Playing renders. It returns a function that empties the slot again.
export const registerNowPlayingTool = (slot, Tool) => {
  if (!TOOLBAR_SLOTS.includes(slot)) {
    throw new Error(`No Now Playing toolbar slot "${slot}"`)
  }
  tools[slot] = Tool
  return () => {
    if (tools[slot] === Tool) delete tools[slot]
  }
}

// registeredTools lists the filled slots, in order, as [slot, Tool].
export const registeredTools = () =>
  TOOLBAR_SLOTS.filter((slot) => tools[slot]).map((slot) => [slot, tools[slot]])
