# Extend Navidrome's existing UI rather than replacing it

The inherited UI is on an old stack (React 17, react-admin 3, MUI v4). Because the fork is never rebased (ADR-0001), replacing it would carry no merge cost. We still chose to extend it: it already has login, browsing, a player, playlists, PWA install and mobile layouts, and rebuilding those would consume most of the build time. New screens (Stations, DJs, Generated Playlists, Downloads) are added in the same stack. If a mobile screen turns out to be poor, we restyle that screen rather than rewrite the frontend.
