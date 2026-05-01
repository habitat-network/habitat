package jetstream

// This package provides a service that fulfills a jetstream-like API for a habitat organization.
// Products that integrate with habitat need a method to receive real-time changes that are relevant
// to their application and index / aggregate them however they want.
//
// This has no authorization primitives attached (like admin controls what org data the app can see).
// This will be added as a follow up.

// The HabitatJetstream services handles listening for updates and fanning them out to many upstream
// jetstream subscribers.
type HabitatJetstream interface {
	Subscribe()
}
