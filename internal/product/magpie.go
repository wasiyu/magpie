//go:build !jellytool

// Package product fixes the application's identity at build time.
package product

const (
	DefaultGatewayPort = 3425
	DefaultGatewayAddr = "127.0.0.1:3425"
	Jellytool          = false
	ID                 = "magpie"
	Name               = "magpie"
	Website            = "https://usemagpie.ai"
	DataName           = "data"
	MarkerName         = ".portable"
)
