package httpapi

import _ "embed"

// subCSS is the subscription page stylesheet (__STC__ is replaced with the
// status colour at render time).
//
//go:embed assets/sub.css
var subCSS string

// zoomJS is the self-hosted map zoom script (served at /map/zoom.js).
//
//go:embed assets/sub_zoom.js
var zoomJS string
