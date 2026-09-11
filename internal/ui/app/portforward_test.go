package app

import (
	"testing"
)

func TestPortForwardInputsAreOnlyPortPairs(t *testing.T) {
	for _, value := range []string{"0:80", "8080:443", "65535:65535"} {
		if got, err := parseForward(value); err != nil || got != value {
			t.Fatalf("%s: %s %v", value, got, err)
		}
	}
	for _, value := range []string{"0.0.0.0:8080:80", "-1:80", "8080:0", "8080:65536", "8080:80;command"} {
		if _, err := parseForward(value); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
}

func TestPortForwardsRemainExplicitlyAttributedAndClose(t *testing.T) {
	m := newTestModel(t)
	stopped := false
	m.forwards = map[int]*forwardSession{1: {cluster: "production", namespace: "team", pod: "api", label: "127.0.0.1:8080 → 80", cancel: func() { stopped = true }}}
	commands := m.forwardCommands()
	found := false
	for _, c := range commands {
		if c.ID == "forward.stop.1" {
			found = true
			if c.Subtitle != "production / team / api" {
				t.Fatal(c.Subtitle)
			}
		}
	}
	if !found {
		t.Fatal("forward has no stop command")
	}
	m.Close()
	if !stopped {
		t.Fatal("connection survived UI exit")
	}
}
