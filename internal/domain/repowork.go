package domain

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Repo work is cloning the owner's repositories, editing them (README cleanup, hygiene fixes) and committing
// locally. The owner has given Griffin standing permission for all of that, so it runs without asking first.
// The one thing that always waits for approval is the push: a push only ever happens in a separate proposed task
// the owner approves.

// RepoWorkDir is where Griffin keeps its own clones, inside the folder the host is allowed to work in.
const RepoWorkDir = "/home/adhyan/Desktop/griffin-work"

var (
	repoWorkRe = regexp.MustCompile(`(?i)\b(readmes?|repo hygiene|repository hygiene|clone (my|the|all|each)|clean ?up (my |the |all |each )?(repos?|repositories|readmes?))\b|/griffin-work/`)
	pushStepRe = regexp.MustCompile(`(?i)\bgit push\b`)
)

// IsRepoWork reports whether a task is repo work: engineering work on the owner's repositories. A stored
// tracker snapshot that merely mentions repo hygiene is not.
func IsRepoWork(t *Task) bool {
	if t == nil || t.ManagerDomain != "engineering" {
		return false
	}
	var p struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(t.InputPayload, &p) == nil && (p.Kind == "tracker_snapshot" || p.Kind == "chat") {
		return false
	}
	text := t.Title + " " + t.Goal
	if r := []rune(text); len(r) > 800 {
		text = string(r[:800])
	}
	return repoWorkRe.MatchString(text)
}

// IsPushStep reports whether a repo-work task is the step that pushes. Only that step needs the owner's approval.
func IsPushStep(t *Task) bool {
	return t != nil && pushStepRe.MatchString(t.Goal)
}

// RepoWorkRules are the standing instructions a repo-work task is shown.
func RepoWorkRules() string {
	return "\n\n---\nHow to do repo work (the owner's standing instructions):\n" +
		"- Work only in " + RepoWorkDir + "/<repo>. If it is not there, clone it with `gh repo clone adhyan-jain/<repo>`. Never edit the owner's other checkouts.\n" +
		"- Make every change on a new branch named `griffin/<short-topic>` and commit it locally with a clear message.\n" +
		"- NEVER run `git push`, never open a pull request, never force anything. Right after cloning run `git remote set-url --push origin no-push-without-approval`, so a stray push fails.\n" +
		"- A README cleanup means: describe what the code actually does (read it, do not guess), give setup and run steps you checked against the repo, remove stale or inflated claims, keep it short. Do not invent features, numbers or links.\n" +
		"- Pushing is a separate step that the owner approves. End your answer with a proposals block with one proposal per repo you committed to, with act set to true and a goal in this form: " +
		"`In " + RepoWorkDir + "/<repo>: restore the push URL with git remote set-url --push origin <the repo's https URL>, then git push -u origin <branch> and open a pull request with gh pr create. Do not change anything else.`\n" +
		"- Report per repo: what changed (diff stat), and what you skipped and why."
}

// RepoWorkKindOf reports whether a request asks for repo cleanup work to be done.
func RepoWorkKindOf(text string) bool {
	return repoWorkRe.MatchString(text) && regexp.MustCompile(`(?i)\b(clean|fix|update|improve|rewrite|tidy|do|run)\b`).MatchString(text)
}

// RepoWorkTaskFor builds the task for a repo cleanup request. The owner's words are the goal.
func RepoWorkTaskFor(request string) (title, goal string) {
	title = "REPO CLEANUP - " + strings.TrimSpace(request)
	if r := []rune(title); len(r) > 80 {
		title = string(r[:80])
	}
	return title, strings.TrimSpace(request)
}
