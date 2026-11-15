package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// DSA item states.
const (
	DsaQueued   = "queued"
	DsaAssigned = "assigned"
	DsaSolved   = "solved"
	DsaFailed   = "failed"
)

// DsaRetryAfterDays is how long a missed problem rests before it is handed out again.
const DsaRetryAfterDays = 7

// DsaItem is one practice problem for one owner. Dates are calendar days in the owner's time zone, kept as
// midnight UTC so that comparing them is plain date arithmetic.
type DsaItem struct {
	Slug       string     `json:"slug"`
	Title      string     `json:"title"`
	Topic      string     `json:"topic"`
	Difficulty string     `json:"difficulty"`
	Ord        int        `json:"ord"`
	State      string     `json:"state"`
	AssignedOn *time.Time `json:"assigned_on,omitempty"`
	DueOn      *time.Time `json:"due_on,omitempty"`
	Attempts   int        `json:"attempts"`
	SolvedAt   *time.Time `json:"solved_at,omitempty"`
}

// URL is the problem's page.
func (i DsaItem) URL() string { return "https://leetcode.com/problems/" + i.Slug + "/" }

// Day returns the calendar day of t in loc, as midnight UTC.
func Day(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// PlanDsaDay brings the list up to date for today and returns today's problems. It changes items in place:
//
//  1. a problem handed out on an earlier day and not solved becomes failed and rests DsaRetryAfterDays
//     after the day it was handed out (that is the "failed a week later" rule);
//  2. if fewer than n problems are assigned for today, it tops up: failed problems whose rest is over come
//     first (oldest due first), then new ones in list order.
//
// Calling it again the same day changes nothing. It returns the slugs of today's problems.
func PlanDsaDay(items []*DsaItem, today time.Time, n int) []string {
	for _, it := range items {
		if it.State == DsaAssigned && it.AssignedOn != nil && it.AssignedOn.Before(today) {
			due := it.AssignedOn.AddDate(0, 0, DsaRetryAfterDays)
			it.State, it.DueOn = DsaFailed, &due
		}
	}
	var current []*DsaItem
	for _, it := range items {
		// A problem solved today still fills one of today's slots; solving early must not hand out more.
		if (it.State == DsaAssigned || it.State == DsaSolved) && it.AssignedOn != nil && it.AssignedOn.Equal(today) {
			current = append(current, it)
		}
	}
	if need := n - len(current); need > 0 {
		var retries, fresh []*DsaItem
		for _, it := range items {
			switch {
			case it.State == DsaFailed && it.DueOn != nil && !it.DueOn.After(today):
				retries = append(retries, it)
			case it.State == DsaQueued:
				fresh = append(fresh, it)
			}
		}
		sort.SliceStable(retries, func(a, b int) bool { return retries[a].DueOn.Before(*retries[b].DueOn) })
		sort.SliceStable(fresh, func(a, b int) bool { return fresh[a].Ord < fresh[b].Ord })
		for _, it := range append(retries, fresh...) {
			if need == 0 {
				break
			}
			d := today
			it.State, it.AssignedOn, it.DueOn = DsaAssigned, &d, nil
			it.Attempts++
			current = append(current, it)
			need--
		}
	}
	sort.SliceStable(current, func(a, b int) bool { return current[a].Ord < current[b].Ord })
	slugs := make([]string, len(current))
	for i, it := range current {
		slugs[i] = it.Slug
	}
	return slugs
}

// DsaNag decides whether to nag and what to say. It stays quiet before fromHour, when nothing is left unsolved
// today, and when nothing was handed out. Later in the evening the wording gets firmer.
func DsaNag(todays []*DsaItem, now time.Time, loc *time.Location, fromHour int) (string, bool) {
	if now.In(loc).Hour() < fromHour {
		return "", false
	}
	var left []string
	for _, it := range todays {
		if it.State == DsaAssigned {
			left = append(left, it.Title)
		}
	}
	if len(left) == 0 {
		return "", false
	}
	msg := fmt.Sprintf("%d DSA problem(s) still open today: %s.", len(left), strings.Join(left, ", "))
	if now.In(loc).Hour() >= 22 {
		msg += " Last call: solve one before you sleep, or it is marked missed and comes back in a week."
	}
	return msg, true
}

// DsaSeed is the starting list, in the order it is handed out (a widely used interview set, easy to hard within each theme).
var DsaSeed = []DsaItem{
	{Slug: "two-sum", Title: "Two Sum", Topic: "arrays", Difficulty: "easy"},
	{Slug: "contains-duplicate", Title: "Contains Duplicate", Topic: "arrays", Difficulty: "easy"},
	{Slug: "valid-anagram", Title: "Valid Anagram", Topic: "strings", Difficulty: "easy"},
	{Slug: "valid-parentheses", Title: "Valid Parentheses", Topic: "stack", Difficulty: "easy"},
	{Slug: "best-time-to-buy-and-sell-stock", Title: "Best Time to Buy and Sell Stock", Topic: "arrays", Difficulty: "easy"},
	{Slug: "binary-search", Title: "Binary Search", Topic: "binary search", Difficulty: "easy"},
	{Slug: "reverse-linked-list", Title: "Reverse Linked List", Topic: "linked list", Difficulty: "easy"},
	{Slug: "merge-two-sorted-lists", Title: "Merge Two Sorted Lists", Topic: "linked list", Difficulty: "easy"},
	{Slug: "linked-list-cycle", Title: "Linked List Cycle", Topic: "linked list", Difficulty: "easy"},
	{Slug: "invert-binary-tree", Title: "Invert Binary Tree", Topic: "trees", Difficulty: "easy"},
	{Slug: "maximum-depth-of-binary-tree", Title: "Maximum Depth of Binary Tree", Topic: "trees", Difficulty: "easy"},
	{Slug: "same-tree", Title: "Same Tree", Topic: "trees", Difficulty: "easy"},
	{Slug: "climbing-stairs", Title: "Climbing Stairs", Topic: "dp", Difficulty: "easy"},
	{Slug: "product-of-array-except-self", Title: "Product of Array Except Self", Topic: "arrays", Difficulty: "medium"},
	{Slug: "maximum-subarray", Title: "Maximum Subarray", Topic: "arrays", Difficulty: "medium"},
	{Slug: "3sum", Title: "3Sum", Topic: "two pointers", Difficulty: "medium"},
	{Slug: "container-with-most-water", Title: "Container With Most Water", Topic: "two pointers", Difficulty: "medium"},
	{Slug: "group-anagrams", Title: "Group Anagrams", Topic: "hashing", Difficulty: "medium"},
	{Slug: "top-k-frequent-elements", Title: "Top K Frequent Elements", Topic: "heap", Difficulty: "medium"},
	{Slug: "longest-substring-without-repeating-characters", Title: "Longest Substring Without Repeating Characters", Topic: "sliding window", Difficulty: "medium"},
	{Slug: "find-minimum-in-rotated-sorted-array", Title: "Find Minimum in Rotated Sorted Array", Topic: "binary search", Difficulty: "medium"},
	{Slug: "search-in-rotated-sorted-array", Title: "Search in Rotated Sorted Array", Topic: "binary search", Difficulty: "medium"},
	{Slug: "remove-nth-node-from-end-of-list", Title: "Remove Nth Node From End of List", Topic: "linked list", Difficulty: "medium"},
	{Slug: "reorder-list", Title: "Reorder List", Topic: "linked list", Difficulty: "medium"},
	{Slug: "binary-tree-level-order-traversal", Title: "Binary Tree Level Order Traversal", Topic: "trees", Difficulty: "medium"},
	{Slug: "validate-binary-search-tree", Title: "Validate Binary Search Tree", Topic: "trees", Difficulty: "medium"},
	{Slug: "kth-smallest-element-in-a-bst", Title: "Kth Smallest Element in a BST", Topic: "trees", Difficulty: "medium"},
	{Slug: "lowest-common-ancestor-of-a-binary-search-tree", Title: "Lowest Common Ancestor of a Binary Search Tree", Topic: "trees", Difficulty: "medium"},
	{Slug: "number-of-islands", Title: "Number of Islands", Topic: "graphs", Difficulty: "medium"},
	{Slug: "clone-graph", Title: "Clone Graph", Topic: "graphs", Difficulty: "medium"},
	{Slug: "course-schedule", Title: "Course Schedule", Topic: "graphs", Difficulty: "medium"},
	{Slug: "pacific-atlantic-water-flow", Title: "Pacific Atlantic Water Flow", Topic: "graphs", Difficulty: "medium"},
	{Slug: "house-robber", Title: "House Robber", Topic: "dp", Difficulty: "medium"},
	{Slug: "coin-change", Title: "Coin Change", Topic: "dp", Difficulty: "medium"},
	{Slug: "longest-increasing-subsequence", Title: "Longest Increasing Subsequence", Topic: "dp", Difficulty: "medium"},
	{Slug: "word-break", Title: "Word Break", Topic: "dp", Difficulty: "medium"},
	{Slug: "decode-ways", Title: "Decode Ways", Topic: "dp", Difficulty: "medium"},
	{Slug: "unique-paths", Title: "Unique Paths", Topic: "dp", Difficulty: "medium"},
	{Slug: "longest-common-subsequence", Title: "Longest Common Subsequence", Topic: "dp", Difficulty: "medium"},
	{Slug: "combination-sum", Title: "Combination Sum", Topic: "backtracking", Difficulty: "medium"},
	{Slug: "subsets", Title: "Subsets", Topic: "backtracking", Difficulty: "medium"},
	{Slug: "permutations", Title: "Permutations", Topic: "backtracking", Difficulty: "medium"},
	{Slug: "word-search", Title: "Word Search", Topic: "backtracking", Difficulty: "medium"},
	{Slug: "merge-intervals", Title: "Merge Intervals", Topic: "intervals", Difficulty: "medium"},
	{Slug: "insert-interval", Title: "Insert Interval", Topic: "intervals", Difficulty: "medium"},
	{Slug: "non-overlapping-intervals", Title: "Non-overlapping Intervals", Topic: "intervals", Difficulty: "medium"},
	{Slug: "rotate-image", Title: "Rotate Image", Topic: "matrix", Difficulty: "medium"},
	{Slug: "spiral-matrix", Title: "Spiral Matrix", Topic: "matrix", Difficulty: "medium"},
	{Slug: "set-matrix-zeroes", Title: "Set Matrix Zeroes", Topic: "matrix", Difficulty: "medium"},
	{Slug: "jump-game", Title: "Jump Game", Topic: "greedy", Difficulty: "medium"},
	{Slug: "min-stack", Title: "Min Stack", Topic: "stack", Difficulty: "medium"},
	{Slug: "daily-temperatures", Title: "Daily Temperatures", Topic: "stack", Difficulty: "medium"},
	{Slug: "kth-largest-element-in-an-array", Title: "Kth Largest Element in an Array", Topic: "heap", Difficulty: "medium"},
	{Slug: "k-closest-points-to-origin", Title: "K Closest Points to Origin", Topic: "heap", Difficulty: "medium"},
	{Slug: "implement-trie-prefix-tree", Title: "Implement Trie (Prefix Tree)", Topic: "trie", Difficulty: "medium"},
	{Slug: "trapping-rain-water", Title: "Trapping Rain Water", Topic: "two pointers", Difficulty: "hard"},
	{Slug: "find-median-from-data-stream", Title: "Find Median from Data Stream", Topic: "heap", Difficulty: "hard"},
}

// DsaSettings is one owner's coach settings.
type DsaSettings struct {
	DailyCount       int    `json:"daily_count"`
	Timezone         string `json:"timezone"`
	LeetcodeUsername string `json:"leetcode_username"`
	NagFromHour      int    `json:"nag_from_hour"`
}

// CleanLeetCodeUsername normalizes a LeetCode username or profile URL (e.g. "https://leetcode.com/u/AdhyanJain/").
func CleanLeetCodeUsername(raw string) string {
	u := strings.TrimSpace(raw)
	u = strings.TrimPrefix(u, "https://leetcode.com/u/")
	u = strings.TrimPrefix(u, "https://leetcode.com/")
	u = strings.TrimPrefix(u, "http://leetcode.com/u/")
	u = strings.TrimPrefix(u, "http://leetcode.com/")
	u = strings.Trim(u, "/")
	return u
}

// DefaultDsaSettings is what a new owner starts with.
func DefaultDsaSettings() DsaSettings {
	return DsaSettings{DailyCount: 2, Timezone: "Asia/Kolkata", LeetcodeUsername: "AdhyanJain", NagFromHour: 18}
}

// Validate checks the settings and that the time zone exists.
func (s DsaSettings) Validate() error {
	clean := CleanLeetCodeUsername(s.LeetcodeUsername)
	if s.DailyCount < 1 || s.DailyCount > 10 || s.NagFromHour < 0 || s.NagFromHour > 23 || len(clean) > 40 {
		return ErrValidation
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		return ErrValidation
	}
	return nil
}
