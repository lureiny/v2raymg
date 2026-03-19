// Package xray provides Xray container implementation.
package xray

// NativeInbound represents Xray native inbound configuration.
type NativeInbound struct {
	JSON []byte
}

// NativeConfig represents Xray native configuration.
type NativeConfig struct {
	JSON []byte
}

// UnmappedWarning represents a warning during mapping.
type UnmappedWarning struct {
	FieldPath string
	Reason    string
	Provider  string
}

// UnmappedWarnings is a collection of warnings.
type UnmappedWarnings []UnmappedWarning

// Empty returns true if there are no warnings.
func (u UnmappedWarnings) Empty() bool { return len(u) == 0 }
