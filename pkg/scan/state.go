package scan

// State represents the lifecycle state of a scan (enforced by persistence-service).
const (
	StatePENDING     = "PENDING"
	StateRUNNING     = "RUNNING"
	StateSUCCESS     = "SUCCESS"
	StateFAILED      = "FAILED"
	StateTIMEOUT     = "TIMEOUT"
	StateUNREACHABLE = "UNREACHABLE"
)

// TerminalStates are states that must not be overwritten by RUNNING or other non-terminal states.
var TerminalStates = map[string]bool{
	StateSUCCESS: true, StateFAILED: true, StateTIMEOUT: true, StateUNREACHABLE: true,
}

// ValidTransition returns true if transitioning from fromState to toState is allowed.
// Allowed: PENDING→RUNNING, RUNNING→SUCCESS|FAILED|TIMEOUT|UNREACHABLE.
// Disallowed: terminal→anything, SUCCESS→RUNNING, FAILED→SUCCESS, etc.
func ValidTransition(fromState, toState string) bool {
	if fromState == "" {
		fromState = StatePENDING
	}
	switch toState {
	case StateRUNNING:
		return fromState == StatePENDING
	case StateSUCCESS, StateFAILED, StateTIMEOUT, StateUNREACHABLE:
		return fromState == StateRUNNING || fromState == StatePENDING
	default:
		return false
	}
}

// IsTerminal returns true if status is a terminal state (must not be downgraded).
func IsTerminal(status string) bool {
	return TerminalStates[status]
}
