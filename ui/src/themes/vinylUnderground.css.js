// The player bar, its queue pop-up and the package's mobile player, drawn in
// the Vinyl Underground palette. `s` is a variant's surfaces and `accent` its
// accent, and `shadow` the one under sheets (see vinylUndergroundPalette.js).
const createStylesheet = ({ mode, s, accent, accentSoft, shadow }) => {
  const main = `.react-jinke-music-player-main.${mode}-theme`
  const mobile = `.react-jinke-music-player-mobile`
  return `
${main} .music-player-panel {
    background-color: ${s.paper};
    color: ${s.text};
    border-top: 1px solid ${s.divider};
    box-shadow: none;
}

${main} .music-player-panel .panel-content .img-content {
    border-radius: 6px;
    box-shadow: none;
}

${main} svg,
${main} .audio-lists-btn,
${main} .music-player-panel .panel-content .player-content .audio-lists-btn {
    color: ${s.text2};
}

${main} svg:active, ${main} svg:hover {
    color: ${accent.main};
}

${main} .music-player-panel .panel-content .player-content .play-btn svg {
    color: ${accent.main};
}

${main} .rc-slider-rail,
${main} .music-player-panel .progress-load-bar {
    background-color: ${s.surface3} !important;
}

${main} .rc-slider-track,
${main} .music-player-panel .panel-content .rc-slider-handle,
${main} .music-player-panel .panel-content .rc-slider-track {
    background-color: ${accent.main} !important;
}

${main} .rc-slider-handle {
    border: none;
    background-color: ${accent.main} !important;
}

${main} .music-player-panel .panel-content .rc-slider-handle:active {
    box-shadow: 0 0 0 4px ${accentSoft};
}

${main} .progress-bar-content .current-time,
${main} .progress-bar-content .duration {
    color: ${s.text2};
}

${main} .audio-lists-btn {
    background-color: ${s.surface2} !important;
    box-shadow: none;
}

${main} .audio-lists-btn:hover, ${main} .audio-lists-btn:active {
    background-color: ${s.surface3} !important;
    color: ${s.text};
}

${main} ::-webkit-scrollbar-thumb {
    background-color: ${s.surface3};
}

${main} .rc-switch-checked {
    background-color: ${accent.main} !important;
    border: 1px solid ${accent.main};
}

${main} .music-player-controller {
    background-color: ${s.paper};
    border-color: ${s.divider};
    color: ${accent.main};
}

${main} .audio-lists-panel {
    background-color: ${s.paper};
    color: ${s.text};
    border: 1px solid ${s.divider};
    border-radius: 12px;
    box-shadow: ${shadow};
}

${main} .audio-lists-panel-header {
    background-color: ${s.paper};
    border-bottom: 1px solid ${s.divider};
    color: ${s.text};
}

${main} .audio-lists-panel .audio-item,
${main} .audio-lists-panel .audio-item:nth-child(odd) {
    background-color: transparent !important;
    color: ${s.text2};
}

${main} .audio-lists-panel .audio-item:hover {
    background-color: ${s.surface2} !important;
    color: ${s.text};
}

${main} .audio-lists-panel .audio-item.playing {
    background-color: ${accentSoft} !important;
}

${main} .audio-lists-panel .audio-item.playing,
${main} .audio-lists-panel .audio-item.playing svg,
${main} .audio-lists-panel .audio-item.playing .player-singer {
    color: ${s.text} !important;
}

${main} .audio-lists-panel .audio-item.playing .player-icons svg {
    color: ${accent.main} !important;
}

${mobile} {
    background-color: ${s.background};
    color: ${s.text};
}

${mobile} .react-jinke-music-player-mobile-header-title,
${mobile} .react-jinke-music-player-mobile-singer-name {
    color: ${s.text};
}

${mobile} .react-jinke-music-player-mobile-cover {
    border-radius: 6px;
    border: none;
    box-shadow: ${shadow};
}

${mobile} .react-jinke-music-player-mobile-cover img.cover {
    border-radius: 6px;
}

${mobile} svg {
    color: ${s.text2};
}

${mobile} .react-jinke-music-player-mobile-operation svg,
${mobile} .react-jinke-music-player-mobile-header-right svg {
    color: ${s.text2};
}

${mobile} .react-jinke-music-player-mobile-toggle .play-btn svg,
${mobile} .react-jinke-music-player-mobile-toggle svg {
    color: ${accent.main};
}

${mobile} .react-jinke-music-player-mobile-progress .current-time,
${mobile} .react-jinke-music-player-mobile-progress .duration {
    color: ${s.text2};
}

${mobile} .rc-slider-rail {
    background-color: ${s.surface3} !important;
}

${mobile} .rc-slider-track,
${mobile} .rc-slider-handle {
    background-color: ${accent.main} !important;
    border: none;
}
`
}

export default createStylesheet
