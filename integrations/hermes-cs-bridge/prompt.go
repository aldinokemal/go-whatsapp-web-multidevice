package main

import _ "embed"

// defaultSystemPrompt is layered on top of the Hermes profile (SOUL, memory) for
// every CS turn. It combines the whatsapp-cs-closing skill with the Ruang Sanad
// closing guide and 30-day script (sources in knowledge/). Override it at runtime
// with CS_SYSTEM_PROMPT_FILE.
//
//go:embed prompts/ruang-sanad-cs.md
var defaultSystemPrompt string
