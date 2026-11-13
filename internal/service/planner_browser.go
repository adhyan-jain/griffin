package service

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/griffin/core/internal/domain"
)

// A request that names one of the owner's own signed-in sites, or is plainly a mail or LinkedIn job, goes to the
// browser hand: it uses the owner's browser as they would. The pattern is narrow on purpose. "Write me a cover
// letter" is still a writing job; only "in my Gmail", "on LinkedIn", "cold mail ..." and the like reach a browser.
var (
	browserSite = regexp.MustCompile(`(?i)\b(in|on|from|via|using|through|open|check|go to)\s+(my\s+)?(gmail|inbox|outlook|linkedin|linked in|google (docs|sheets|calendar)|naukri|internshala|wellfound|unstop)\b`)
	browserJob  = regexp.MustCompile(`(?i)\b(cold[- ]?(mail|email|dm|message|outreach)|linkedin (message|connection|dm|outreach)|connect (with|to) .{0,40} on linkedin|apply (to|for) .{0,60} (on|via|through) (linkedin|naukri|internshala|wellfound|unstop))\b`)
)

func wantsBrowser(text string) bool {
	if buildingVerb.MatchString(strings.ToLower(text)) {
		return false
	}
	return browserSite.MatchString(text) || browserJob.MatchString(text)
}

// browserSteps plans one browser step. The agent prepares the work and stops before anything is sent; the
// owner approves the exact script (what will be typed and which button ends it), and only then does it go.
func browserSteps(req *domain.UserRequest) ([]domain.ChildTaskPlan, []domain.PlanDependency) {
	b, _ := json.Marshal(map[string]any{
		"requires": []string{domain.HostCapBrowser},
		"browser":  domain.BrowserAction{Goal: req.RequestText},
	})
	return []domain.ChildTaskPlan{{
		Key: "browser_job", Title: truncateString("Browser: "+req.RequestText, 60), Goal: req.RequestText,
		ManagerDomain: "personal_ops", InputPayload: b, Priority: req.Priority,
	}}, nil
}

var phoneJob = regexp.MustCompile(`(?i)\b(on|from|using|with) my phone\b|\bon the phone\b|\bin the (\w+ ){0,2}app on my phone\b`)

// wantsPhone is true when the owner asks for something to be done on their phone's screen. WhatsApp and Instagram
// have their own, earlier routes; this is for every other app.
func wantsPhone(text string) bool {
	if buildingVerb.MatchString(strings.ToLower(text)) {
		return false
	}
	return phoneJob.MatchString(text)
}

// phoneSteps plans one phone step: the phone works the goal on its screen, stops before anything is sent, and
// the owner approves the exact steps.
func phoneSteps(req *domain.UserRequest) ([]domain.ChildTaskPlan, []domain.PlanDependency) {
	b, _ := json.Marshal(map[string]any{
		"requires": []string{domain.HostCapPhoneUI},
		"phone":    domain.BrowserAction{Goal: req.RequestText},
	})
	return []domain.ChildTaskPlan{{
		Key: "phone_job", Title: truncateString("Phone: "+req.RequestText, 60), Goal: req.RequestText,
		ManagerDomain: "personal_ops", InputPayload: b, Priority: req.Priority,
	}}, nil
}
