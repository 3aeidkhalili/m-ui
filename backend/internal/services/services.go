// Package services holds the business logic ported from the former Python
// service layer: settings, protocols, outbounds, provisioning, sync, traffic,
// client-config generation, connections, protocol stats and geolocation.
package services

import (
	"multivpn/internal/config"
	"multivpn/internal/xray"
)

// Shared, process-wide dependencies (mirrors the former module-level
// `settings` singleton). Set once at startup by Init.
var (
	Cfg  *config.Config
	Geo  *GeoIP
	Xray *xray.Client
)

// Init wires the shared dependencies used across the service functions.
func Init(cfg *config.Config, geo *GeoIP, xrayClient *xray.Client) {
	Cfg = cfg
	Geo = geo
	Xray = xrayClient
}
