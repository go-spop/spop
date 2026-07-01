package spec

const Version20 = "2.0"

var SupportedVersions = []string{Version20}

const (
	// MinFrameSize is the minimum allowed frame size per the SPOE spec.
	MinFrameSize = 256

	// DefaultMaxFrameSize is the agent's default maximum frame size.
	DefaultMaxFrameSize = 16 * 1024
)
