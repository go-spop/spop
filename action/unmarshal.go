package action

import (
	"errors"
	"fmt"

	"github.com/go-spop/spop/typeddata"
	"github.com/go-spop/spop/varint"
)

// ErrMalformedAction reports a LIST-OF-ACTIONS that does not parse against
// section 3.4's grammar.
var ErrMalformedAction = errors.New("malformed action")

// Unmarshal decodes a LIST-OF-ACTIONS.
//
// Section 3.4 gives the list no item count, so entries are read back to back
// until the buffer is exhausted, exactly as Marshal writes them. Every length
// the peer declares is checked against what is left before it is used: the
// payload is peer-chosen, so a truncated or oversized field is malformed input
// rather than a slice expression to trust.
func Unmarshal(buf []byte) (Actions, error) {
	actions := make(Actions, 0)

	for len(buf) > 0 {
		var action Action

		action.Type = Type(buf[0])
		buf = buf[1:]

		var wantArgs byte

		switch action.Type {
		case TypeSetVar:
			wantArgs = nbVarsSetVar

		case TypeUnsetVar:
			wantArgs = nbVarsUnsetVar

		default:
			return nil, fmt.Errorf("%w: unknown action type %d", ErrMalformedAction, action.Type)
		}

		if len(buf) == 0 {
			return nil, fmt.Errorf("%w: missing NB-ARGS", ErrMalformedAction)
		}

		if buf[0] != wantArgs {
			return nil, fmt.Errorf("%w: action type %d takes %d arguments, got %d", ErrMalformedAction, action.Type, wantArgs, buf[0])
		}

		buf = buf[1:]

		if len(buf) == 0 {
			return nil, fmt.Errorf("%w: missing VAR-SCOPE", ErrMalformedAction)
		}

		action.Scope = Scope(buf[0])

		switch action.Scope {
		case ScopeProcess, ScopeSession, ScopeTransaction, ScopeRequest, ScopeResponse:
		default:
			return nil, fmt.Errorf("%w: unknown scope %d", ErrMalformedAction, action.Scope)
		}

		buf = buf[1:]

		// VAR-NAME is a bare length-prefixed string, not a TYPED-DATA one,
		// which is what Marshal writes and what section 3.4's <STRING> means
		// here.
		nameLen, n := varint.Uvarint(buf)
		if n < 0 {
			return nil, fmt.Errorf("%w: truncated VAR-NAME length", ErrMalformedAction)
		}

		buf = buf[n:]

		if uint64(len(buf)) < nameLen {
			return nil, fmt.Errorf("%w: VAR-NAME of %d bytes exceeds the %d remaining", ErrMalformedAction, nameLen, len(buf))
		}

		action.Name = string(buf[:nameLen])
		buf = buf[nameLen:]

		if action.Type == TypeUnsetVar {
			actions = append(actions, action)
			continue
		}

		value, n, err := typeddata.Decode(buf)
		if err != nil {
			return nil, fmt.Errorf("%w: VAR-VALUE: %w", ErrMalformedAction, err)
		}

		action.Value = value
		buf = buf[n:]

		actions = append(actions, action)
	}

	return actions, nil
}
