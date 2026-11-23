// Command lendspare is a small program that lets a person offer what their computer can do -
// folders for the websites they trust, and spare compute - to the providers they choose to
// connect to.
//
// A single, dependency-free (stdlib-only) cross-platform binary.
//
// Starting LendSpare contacts no provider and shares nothing. A connection exists only after the
// person confirmed it on this computer (internal/app/connect.go); each connection has its own
// token, its own scopes and its own switches, and one scheduler (internal/app/scheduler.go)
// keeps compute to one job at a time across all of them.
//
//   - comfy:    image workflows on a curated ComfyUI container (or the owner's own ComfyUI)
//   - llm:      open-weight chat models the owner already runs in Ollama / LM Studio
//   - pipeline: a provider's container, sandboxed: no host mounts but a per-job directory,
//     egress only through an allow-list proxy, API keys from a local env file that is
//     never sent anywhere
//   - grader, features, tts: further container kinds
//
// The owner's own use of the GPU always wins: while a foreign process uses the card LendSpare claims
// no GPU work and stops the ComfyUI container.
//
// Run:
//
//	lendspare                       the tray icon (desktop) or the console
//	lendspare connect               connect to a provider from the terminal
//	lendspare connections           list the connections
//	lendspare disconnect <id>       remove one
//	lendspare --version
package main

import "github.com/LendSpare/lendspare/internal/app"

func main() { app.Main() }
