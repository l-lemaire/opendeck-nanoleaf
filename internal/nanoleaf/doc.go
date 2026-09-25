// Package nanoleaf talks to Nanoleaf devices (Light Panels, Canvas, Shapes,
// Elements, Lines) over their local HTTP API, the "Open API".
//
// Unlike Philips Hue there is no bridge: every installation is one device
// on the network, reached directly at http://<host>:16021/api/v1/. There is
// no TLS. Authentication is a token obtained once by holding the device's
// power button (see pair.go), then placed in every URL path:
//
//	GET  /api/v1/<token>/            everything about the device
//	GET  /api/v1/<token>/state/on    {"value": true}
//	PUT  /api/v1/<token>/state       {"on": {"value": false}}
//
// Because the token sits in the URL, the debug logger redacts URL paths
// (httplog.go) rather than a header.
//
// Files:
//
//	dnsmsg.go     minimal DNS codec (shared with the Hue plugin)
//	mdns.go       find devices: service _nanoleafapi._tcp
//	discover.go   the Device type and the Discovery options
//	httplog.go    debug dump of every HTTP exchange, token redacted
//	client.go     the HTTP client and the state calls (step 2+)
//	pair.go       obtaining a token (step 2)
//	sse.go        Server-Sent Events parser for live updates (step 4)
package nanoleaf
