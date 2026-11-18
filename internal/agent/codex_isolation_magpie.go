//go:build !jellytool

package agent

// The upstream build retains its original provider identity and wiring.
const codexProviderID = magpieID

func codexPeerID() string { return "jellytool" }

func codexPeerActive(string, string) bool { return false }

func codexIsolated(a *Agent, _ string) *Agent { return a }
