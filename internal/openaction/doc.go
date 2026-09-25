// Package openaction implements the plugin side of the OpenAction protocol,
// the API OpenDeck exposes to plugins. It is backwards compatible with the
// Elgato Stream Deck plugin protocol, so the same code would talk to the
// official Stream Deck software.
//
// How a plugin comes to life:
//
//  1. The host (OpenDeck) starts the plugin executable with four arguments:
//     -port <n> -pluginUUID <id> -registerEvent <name> -info <json>
//  2. The plugin opens a WebSocket to ws://127.0.0.1:<port>.
//  3. It sends one registration message: {"event": <name>, "uuid": <id>}.
//  4. From then on both sides exchange JSON text messages, each with an
//     "event" field. The host tells the plugin about buttons appearing,
//     keys being pressed and settings changing; the plugin asks the host to
//     change a button's state, title or image, or to store settings.
//
// Files:
//
//	args.go      parsing the command line and the -info JSON
//	events.go    the message shapes, incoming and outgoing
//	conn.go      the WebSocket connection: register, receive, send helpers
//	run.go       the dispatch loop that routes events to handler functions
//
// Each button on the deck is an "instance" of an action and is identified by
// an opaque "context" string chosen by the host. Every incoming event about
// a button carries its context, and every outgoing request that targets a
// button must echo it back.
package openaction
