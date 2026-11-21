package domain

import (
	"encoding/json"
	"regexp"
	"strings"
)

// An initiative task looks at one of the owner's lists and the owner's profile, decides what the owner should do
// next without being asked, and ends its answer with proposals. Each proposal becomes a task that waits on the
// owner's Approvals page: approve it and it runs, reject it and it is dropped.

// InitiativeMarker is the word in a task's title that marks it as an initiative task.
const InitiativeMarker = "INITIATIVE"

// ProposalApprovalAction is the approval type for a proposed task.
const ProposalApprovalAction = "task_proposal"

// Proposal is one follow-up task an initiative task suggests.
type Proposal struct {
	Title string `json:"title"`
	Why   string `json:"why"`
	Goal  string `json:"goal"`
	// Act marks a proposal whose approval also grants the act tier for that one task (a push, for example).
	Act bool `json:"act"`
}

const maxProposals = 8

var proposalsBlock = regexp.MustCompile("(?s)```proposals\\s*(\\[.*?\\])\\s*```")

// IsInitiativeTask reports whether a task is an initiative task, from its title.
func IsInitiativeTask(title string) bool {
	return strings.Contains(strings.ToUpper(title), InitiativeMarker)
}

// IsProposingTask reports whether a task's answer may end with proposals: an initiative task, or a task whose
// payload says it proposes.
func IsProposingTask(t *Task) bool {
	if t == nil {
		return false
	}
	if IsInitiativeTask(t.Title) {
		return true
	}
	var p struct {
		Proposes bool `json:"proposes"`
	}
	return json.Unmarshal(t.InputPayload, &p) == nil && p.Proposes
}

// ParseProposals reads the proposals block an initiative task ends its answer with. Entries without a title or a
// goal are dropped and the list is capped, so a malformed answer never creates junk.
func ParseProposals(text string) []Proposal {
	m := proposalsBlock.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	var all []Proposal
	if json.Unmarshal([]byte(m[1]), &all) != nil {
		return nil
	}
	var out []Proposal
	for _, p := range all {
		p.Title, p.Goal, p.Why = strings.TrimSpace(p.Title), strings.TrimSpace(p.Goal), strings.TrimSpace(p.Why)
		if p.Title == "" || p.Goal == "" {
			continue
		}
		if r := []rune(p.Title); len(r) > 80 {
			p.Title = string(r[:80])
		}
		if r := []rune(p.Goal); len(r) > 1500 {
			p.Goal = string(r[:1500])
		}
		out = append(out, p)
		if len(out) == maxProposals {
			break
		}
	}
	return out
}

var (
	initiativeInternships = regexp.MustCompile(`(?i)\binternships?\b`)
	initiativeHackathons  = regexp.MustCompile(`(?i)\bhackathons?\b`)
	initiativeIntent      = regexp.MustCompile(`(?i)\b(initiative|what (should|do) i (do|target)|next steps?|expand|more (internships|options)|haven'?t applied|not applied|referrals?|easy win|easiest|best (ones|bets)|which (ones|hackathons?) (should|to)|target)\b`)
)

// InitiativeKindOf reports whether a request asks Griffin to decide what to do next about a list ("take
// initiative on my internships", "which hackathons are easy wins"), and about which list.
func InitiativeKindOf(text string) string {
	if !initiativeIntent.MatchString(text) {
		return ""
	}
	switch {
	case initiativeHackathons.MatchString(text):
		return "hackathons"
	case initiativeInternships.MatchString(text):
		return "internships"
	}
	return ""
}

const proposalFormat = "\n\nEnd your answer with the proposals, in one fenced block exactly like this (valid JSON, at most 6 entries, most valuable first):\n" +
	"```proposals\n[{\"title\":\"short action title\",\"why\":\"one sentence on why it is worth doing\",\"goal\":\"the full instruction for the task, written so it can run on its own\"}]\n```\n" +
	"Each proposal is held for the owner's approval before it runs, so propose real actions, not questions. Use only facts from the list and the verified profile: do not invent companies, dates, prizes or numbers; say 'unknown, verify' for missing fields."

// InitiativeTaskFor builds the task that decides what the owner should do next about a list.
func InitiativeTaskFor(kind string) (title, goal string) {
	switch kind {
	case "hackathons":
		title = "HACKATHON INITIATIVE - the easiest wins worth targeting"
		goal = "Look at the hackathons on the shared Hackathons list and the owner's verified profile, and decide which ones to target. " +
			"Judge by where winning is realistic and the money is good, not by what the owner already knows: prize pool against how crowded the event is, " +
			"online or nearby, beginner or student tracks, smaller or less famous events, enough time left to build something real, and how well the theme fits the owner's real projects. " +
			"Rank the top five in a table (hackathon, prize, deadline, why it is an easy win, effort, odds as high/medium/low) and say plainly which one or two to do first. " +
			"Mark anything the list does not say as unknown. Propose one task per hackathon worth entering: registering, picking the idea from the owner's real projects, and a build plan." + proposalFormat
	default:
		title = "INTERNSHIP INITIATIVE - what to do next on the list"
		goal = "Look at the shared Internships list and the owner's verified profile, and decide what the owner should do next, without waiting to be asked. " +
			"First, name the listings that are still open and not applied to, by tier. " +
			"Propose: (1) preparing the application drafts for those, using exactly the sentence 'prepare the application drafts for all the A tier internships on my list and get them ready for my approval' (and the same with B tier if there are open B tier listings); " +
			"(2) LinkedIn referral requests for the best open listings: find 2-3 real people at the company who could refer the owner, in this order: VIT alumni from ACM, ISTE, MFC or TAM, then other VIT alumni, then other developers (Indian developers first for a foreign company); draft a 3-4 sentence message for each that mentions the owner's real projects briefly, and queue each for approval before sending; " +
			"(3) emailing a recruiter or an engineer where that fits; " +
			"(4) ways to grow the list: sources or searches that would surface more internships that fit the profile, as a task that adds new rows to the Internships list. " +
			"Stay on applying and maintaining the list: do not propose interview prep, posts, projects or anything else for the owner's own preparation. " +
			"Keep the first paragraph to what matters most today." + proposalFormat
	}
	return title, goal
}
