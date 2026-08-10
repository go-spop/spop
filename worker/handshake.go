package worker

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/go-spop/spop/frame"
)

const (
	// The SPOP version this agent speaks, in section 3.2.5's "Major.Minor" form.
	versionMajor = 2
	versionMinor = 0

	// minFrameSize is the floor section 3.2 puts on the maximum frame size a
	// peer may support: "The maximum size supported by peers for a frame must
	// be greater than or equal to 256 bytes."
	minFrameSize = 256
)

// version is the "Major.Minor" string announced in the AGENT-HELLO.
var version = strconv.Itoa(versionMajor) + "." + strconv.Itoa(versionMinor)

// disconnectError is a handshake failure the peer is entitled to be told about,
// carrying the section 3.5 status code that names it.
type disconnectError struct {
	code    uint32
	message string
}

func (e *disconnectError) Error() string {
	return fmt.Sprintf("%s (status code %d)", e.message, e.code)
}

// negotiated is what the agent answers a HAPROXY-HELLO with, once the frame has
// been checked against section 3.2.4.
type negotiated struct {
	maxFrameSize uint32
	capabilities []string
}

// negotiate validates a HAPROXY-HELLO and works out the AGENT-HELLO's reply.
// Section 3.2.4 requires an AGENT-DISCONNECT rather than an AGENT-HELLO when an
// item is missing or an incompatibility is detected.
//
// Each mandatory item is read with a type assertion: a KV-VALUE carries its own
// TYPED-DATA type nibble, so a peer chooses the Go type and a wrong one must be
// reported rather than trusted. A present-but-mistyped item is treated as an
// unusable value, which is the same "value not found" the codes describe.
func negotiate(f *frame.Frame) (negotiated, *disconnectError) {
	var out negotiated

	versions, ok := stringItem(f, "supported-versions")
	if !ok {
		return out, &disconnectError{statusCodeNoVersion, "no usable supported-versions in the HAProxyHello"}
	}

	if !versionSupported(versions) {
		return out, &disconnectError{
			statusCodeBadVersion,
			fmt.Sprintf("agent speaks SPOP %s, HAProxy announced %q", version, versions),
		}
	}

	value, found := f.KV.Get("max-frame-size")
	if !found {
		return out, &disconnectError{statusCodeNoMaxFrameSize, "no max-frame-size in the HAProxyHello"}
	}

	peerFrameSize, ok := value.(uint32)
	if !ok {
		return out, &disconnectError{statusCodeNoMaxFrameSize, "no usable max-frame-size in the HAProxyHello"}
	}

	if peerFrameSize < minFrameSize {
		return out, &disconnectError{
			statusCodeBadFrameSize,
			fmt.Sprintf("max-frame-size %d is below the %d byte minimum", peerFrameSize, minFrameSize),
		}
	}

	peerCapabilities, ok := stringItem(f, "capabilities")
	if !ok {
		return out, &disconnectError{statusCodeNoCapabilities, "no usable capabilities in the HAProxyHello"}
	}

	// Section 3.2.4: announce "the lower value between its maximum size allowed
	// for a frame and the HAProxy one".
	out.maxFrameSize = peerFrameSize
	if out.maxFrameSize > frame.MaxFrameSize {
		out.maxFrameSize = frame.MaxFrameSize
	}

	out.capabilities = splitList(peerCapabilities)

	return out, nil
}

// stringItem reads a mandatory STRING item, reporting absence and a peer-chosen
// wrong type the same way.
func stringItem(f *frame.Frame, name string) (string, bool) {
	value, found := f.KV.Get(name)
	if !found {
		return "", false
	}

	s, ok := value.(string)

	return s, ok
}

// versionSupported reports whether this agent's version is one HAProxy will
// accept. Section 3.2.5: the agent's version "must be lower or equal than one
// of major versions announced by HAProxy". Entries that do not parse are
// ignored rather than failing the whole list, so an unrecognised future format
// alongside a usable version still negotiates.
func versionSupported(list string) bool {
	for _, entry := range splitList(list) {
		major, minor, ok := parseVersion(entry)
		if !ok {
			continue
		}

		if major > versionMajor || (major == versionMajor && minor >= versionMinor) {
			return true
		}
	}

	return false
}

func parseVersion(entry string) (major, minor int, ok bool) {
	number, fraction, found := strings.Cut(entry, ".")
	if !found {
		return 0, 0, false
	}

	major, err := strconv.Atoi(number)
	if err != nil {
		return 0, 0, false
	}

	minor, err = strconv.Atoi(fraction)
	if err != nil {
		return 0, 0, false
	}

	return major, minor, true
}

// splitList splits a comma-separated item. Section 3.2.4 says spaces must be
// ignored, if any.
func splitList(list string) []string {
	if strings.TrimSpace(list) == "" {
		return nil
	}

	parts := strings.Split(list, ",")

	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}

	return out
}
