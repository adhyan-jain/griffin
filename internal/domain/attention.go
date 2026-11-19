package domain

// AttentionLevel defines how aggressively Griffin should seek the owner's attention.
type AttentionLevel string

const (
	AttentionCriticalInterrupt AttentionLevel = "critical_interrupt"  // Immediate audio interruption / wake phone
	AttentionHighPriorityBrief AttentionLevel = "high_priority_brief" // Surface in morning/evening brief + push notif
	AttentionPassiveInfo       AttentionLevel = "passive_info"        // Background status log
)

// HostPresenceState represents where and how the owner is currently interacting.
type HostPresenceState string

const (
	HostStateLaptopActive HostPresenceState = "laptop_active"
	HostStatePhoneActive  HostPresenceState = "phone_active"
	HostStateAway         HostPresenceState = "away"
	HostStateInMeeting    HostPresenceState = "in_meeting"
)

// AttentionAction is the resulting routing decision for an inbound alert.
type AttentionAction struct {
	Level              AttentionLevel `json:"level"`
	ShouldSpeakVoice   bool           `json:"should_speak_voice"`
	ShouldInterrupt    bool           `json:"should_interrupt"`
	PreferredOutput    string         `json:"preferred_output"` // phone_earbuds, phone_speaker, laptop_ui, silent_push
	SpokenNotification string         `json:"spoken_notification,omitempty"`
}

// EvaluateAttention determines whether to interrupt the owner based on event urgency and host presence.
func EvaluateAttention(urgency string, presence HostPresenceState, summary string) AttentionAction {
	switch urgency {
	case "critical", "assessment_deadline", "interview_request":
		if presence == HostStateInMeeting {
			return AttentionAction{
				Level:            AttentionCriticalInterrupt,
				ShouldSpeakVoice: false,
				ShouldInterrupt:  false,
				PreferredOutput:  "silent_push",
			}
		}
		if presence == HostStateAway || presence == HostStatePhoneActive {
			return AttentionAction{
				Level:              AttentionCriticalInterrupt,
				ShouldSpeakVoice:   true,
				ShouldInterrupt:    true,
				PreferredOutput:    "phone_earbuds",
				SpokenNotification: summary,
			}
		}
		return AttentionAction{
			Level:            AttentionCriticalInterrupt,
			ShouldSpeakVoice: false,
			ShouldInterrupt:  true,
			PreferredOutput:  "laptop_ui",
		}

	case "high", "new_opportunity", "approval_required":
		if presence == HostStateAway {
			return AttentionAction{
				Level:            AttentionHighPriorityBrief,
				ShouldSpeakVoice: false,
				ShouldInterrupt:  false,
				PreferredOutput:  "silent_push",
			}
		}
		return AttentionAction{
			Level:            AttentionHighPriorityBrief,
			ShouldSpeakVoice: false,
			ShouldInterrupt:  false,
			PreferredOutput:  "laptop_ui",
		}

	default:
		return AttentionAction{
			Level:            AttentionPassiveInfo,
			ShouldSpeakVoice: false,
			ShouldInterrupt:  false,
			PreferredOutput:  "background_log",
		}
	}
}
