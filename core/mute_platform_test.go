package core

import "testing"

// Regression for patch 8: explicit outbound sends (`cc-connect send`) must
// bypass the mutePlatform wrapper attached to sessions spawned by muted
// timer/cron jobs. Before the fix, send -s inside such a turn returned
// success while the wrapper silently dropped the message.
func TestUnwrapMutePlatform(t *testing.T) {
	real := &stubPlatformForMute{name: "feishu"}
	wrapped := &mutePlatform{Platform: real}

	if got := unwrapMutePlatform(wrapped); got != real {
		t.Fatalf("unwrapMutePlatform(mutePlatform) = %v, want inner platform", got)
	}
	if got := unwrapMutePlatform(real); got != real {
		t.Fatalf("unwrapMutePlatform(real) = %v, want unchanged", got)
	}
	if got := unwrapMutePlatform(nil); got != nil {
		t.Fatalf("unwrapMutePlatform(nil) = %v, want nil", got)
	}
}

type stubPlatformForMute struct {
	Platform
	name string
}

func (s *stubPlatformForMute) Name() string { return s.name }
