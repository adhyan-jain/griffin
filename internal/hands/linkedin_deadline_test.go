package hands

import (
	"testing"
	"time"
)

func TestALinkedInScanHasAHardTimeLimit(t *testing.T) {
	if got := LinkedInDeadline(10); got != 20*time.Minute {
		t.Fatalf("a 10-minute scan stops by 20 minutes: %v", got)
	}
	if got := LinkedInDeadline(0); got != 20*time.Minute {
		t.Fatalf("an unset budget still has a limit: %v", got)
	}
}
