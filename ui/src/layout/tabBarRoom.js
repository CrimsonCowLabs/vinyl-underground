// The room the phone's bottom tab bar takes, above the home indicator. The
// mini player and page content sit above it (MiniPlayer, usePlayerRoom).
export const TAB_BAR_HEIGHT = 60

// A CSS length: px more than the tab bar and the home indicator below it
export const aboveTabBar = (px) =>
  `calc(env(safe-area-inset-bottom) + ${TAB_BAR_HEIGHT + px}px)`
