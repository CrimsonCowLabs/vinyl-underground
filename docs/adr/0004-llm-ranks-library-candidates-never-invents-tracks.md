# The LLM shapes and ranks candidates from the Library; it never names Tracks itself

A small local LLM (CPU-only on typical home hardware) can't hear audio and doesn't reliably know what a given Library contains. So for Generated Playlists and Stations, the LLM first turns the Prompt or Theme plus the DJ persona into structured filters and seed artists. The server builds a candidate list of real Tracks from the Library (tags, Listener history, Last.fm similar artists). The LLM then ranks or trims that list and writes Blurbs. Every Track it outputs must come from the candidate list, so it can never suggest music the Listener doesn't own.

## Consequences

- The server talks to any OpenAI-compatible endpoint (Ollama, llama.cpp, LM Studio), set by a URL and model name in config. With no LLM configured, AI features are hidden, and Stations still run with rule-based selection and no Blurbs.
- Because LLM calls are slow on CPU, Stations choose Tracks and generate Blurbs 2–3 Tracks ahead.
- Using Last.fm metadata (`EnableExternalServices`) sends artist and album names off the machine. Scrobbling stays off.
