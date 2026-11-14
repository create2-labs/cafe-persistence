package scan

import "testing"

func TestValidTransition(t *testing.T) {
	tests := []struct {
		from, to string
		want     bool
	}{
		{StatePENDING, StateRUNNING, true},
		{"", StateRUNNING, true},
		{StateRUNNING, StateSUCCESS, true},
		{StateRUNNING, StateFAILED, true},
		{StateRUNNING, StateTIMEOUT, true},
		{StateRUNNING, StateUNREACHABLE, true},
		{StatePENDING, StateSUCCESS, true},
		{StatePENDING, StateFAILED, true},
		{StateSUCCESS, StateRUNNING, false},
		{StateSUCCESS, StateSUCCESS, false},
		{StateSUCCESS, StateFAILED, false},
		{StateFAILED, StateSUCCESS, false},
		{StateFAILED, StateRUNNING, false},
		{StateTIMEOUT, StateRUNNING, false},
		{StateUNREACHABLE, StateSUCCESS, false},
	}
	for _, tt := range tests {
		got := ValidTransition(tt.from, tt.to)
		if got != tt.want {
			t.Errorf("ValidTransition(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}

func TestIsTerminal(t *testing.T) {
	if !IsTerminal(StateSUCCESS) || !IsTerminal(StateFAILED) || !IsTerminal(StateTIMEOUT) || !IsTerminal(StateUNREACHABLE) {
		t.Error("terminal states should be terminal")
	}
	if IsTerminal(StatePENDING) || IsTerminal(StateRUNNING) || IsTerminal("") {
		t.Error("non-terminal states should not be terminal")
	}
}
