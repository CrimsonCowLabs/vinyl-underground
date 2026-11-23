// stubResizeObserver stands in for ResizeObserver, which jsdom lacks, until
// the returned restore is called. A test calls an observer's callback itself,
// as the browser would on a resize. For tests only.
export const stubResizeObserver = () => {
  const saved = window.ResizeObserver
  const observers = new Set()
  window.ResizeObserver = class {
    constructor(callback) {
      this.callback = callback
    }
    observe() {
      observers.add(this)
    }
    unobserve() {}
    disconnect() {
      observers.delete(this)
    }
  }
  const restore = () => {
    window.ResizeObserver = saved
  }
  return { observers, restore }
}
