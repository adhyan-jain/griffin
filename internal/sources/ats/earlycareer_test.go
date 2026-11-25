package ats

import (
	"testing"

	"github.com/griffin/core/internal/jobs"
)

func TestEarlyCareerTitles(t *testing.T) {
	yes := []string{
		"Intern - Frontend",
		"Software Engineer Intern",
		"Software Engineering Intern (Summer 2027)",
		"Machine Learning Intern, Research",
		"Summer 2027 Software Engineering Internship",
		"Engineering Internship",
		"SDE Intern",
		"Backend Developer Intern - Bengaluru",
		"Research Engineer Intern",
		"Data Science Interns",
		"Intern/Co-op, Developer Tools",
		"Software Engineer Co-op (Fall 2026)",
		"Software Engineer Coop",
		"Software Engineering Apprentice",
		"Engineering Apprenticeship Programme",
		"Graduate Engineer Trainee",
		"Associate Software Engineer Trainee",
		"Software Engineer, New Grad",
		"Software Engineer - New Grad 2027",
		"Software Engineer, University Graduate",
		"Software Engineer (University Hire)",
		"2028 Graduate Software Engineer",
		"Class of 2027 - Software Engineer",
		"Graduate Software Engineer",
		"Early Career Software Engineer",
		"Entry Level Backend Engineer",
		"Campus Hire - SDE 1",
		"Fresher - QA Engineer",
		"Werkstudent Software Engineering",
		"Working Student, Platform Engineering",
		"Student Software Engineer",
		"Student Researcher, Google DeepMind",
		"Sales Engineer Intern",
		"Data Engineer Intern, Marketing Analytics",
		"Design Intern, Product",
		"DevOps Intern",
		"AI Research Intern",
	}
	for _, title := range yes {
		if !IsEarlyCareer(jobs.RawJob{Title: title}) {
			t.Errorf("%q is an early-career role", title)
		}
	}

	no := []string{
		"Internal Tools Engineer",
		"Software Engineer, Internal Tools",
		"International Payments Engineer",
		"Internet Infrastructure Engineer",
		"Senior Software Engineer",
		"Staff Backend Engineer",
		"Engineering Manager",
		"Principal Engineer, Distributed Systems",
		"Site Reliability Engineer",
		"Director of Engineering",
		"Frontend Engineer",
		"Machine Learning Engineer",
		"Product Manager",
		"Solutions Architect",
		"Student Success Manager",
		"Student Loan Operations Analyst",
		"Intern Experience Program Coordinator, Recruiting",
		"University Recruiter",
		"Technical Recruiter",
		"Sales Intern",
		"Marketing Intern",
		"Legal Intern",
		"Finance Intern",
		"HR Intern",
		"People Operations Intern",
		"Account Executive, Enterprise",
		"Business Development Intern",
		"Customer Success Intern",
		"Content Marketing Intern",
		"",
	}
	for _, title := range no {
		if IsEarlyCareer(jobs.RawJob{Title: title}) {
			t.Errorf("%q is not an early-career engineering role (%s)", title, EarlyCareerReason(jobs.RawJob{Title: title}))
		}
	}
}

func TestEarlyCareerFromEmploymentTypeAndDescription(t *testing.T) {
	cases := []struct {
		name string
		job  jobs.RawJob
		want bool
	}{
		{"employment type intern", jobs.RawJob{Title: "Platform Engineer", EmploymentType: "Intern"}, true},
		{"employment type internship", jobs.RawJob{Title: "Software Engineer", EmploymentType: "Internship"}, true},
		{"employment type co-op", jobs.RawJob{Title: "Software Engineer", EmploymentType: "Co-op"}, true},
		{"employment type full time", jobs.RawJob{Title: "Software Engineer", EmploymentType: "Full-time"}, false},
		{"employment type is not a substring match", jobs.RawJob{Title: "Software Engineer", EmploymentType: "Internal transfer"}, false},
		{"this internship", jobs.RawJob{Title: "Software Engineer", Description: "In this internship you will build features with the payments team."}, true},
		{"summer 2027 internship", jobs.RawJob{Title: "Engineer", Description: "Join our Summer 2027 internship cohort."}, true},
		{"class of 2027", jobs.RawJob{Title: "Software Engineer", Description: "Open to the class of 2027."}, true},
		{"2028 graduates", jobs.RawJob{Title: "Software Engineer", Description: "We welcome 2028 graduates."}, true},
		{"new grad in description", jobs.RawJob{Title: "Software Engineer", Description: "This is a new grad role."}, true},
		{"currently enrolled", jobs.RawJob{Title: "Researcher", Description: "You are currently enrolled in a university degree programme."}, true},
		{"a degree from a university is not enough", jobs.RawJob{Title: "Software Engineer", Description: "A degree from an accredited university and 5 years of experience."}, false},
		{"mentoring interns is not an internship", jobs.RawJob{Title: "Staff Engineer", Description: "You will mentor interns and junior engineers."}, false},
		{"internal tools in description", jobs.RawJob{Title: "Software Engineer", Description: "You will build internal tools for the finance team."}, false},
		{"non-engineering department", jobs.RawJob{Title: "Intern", Department: "Sales"}, false},
		{"engineering title beats a sales department", jobs.RawJob{Title: "Solutions Engineer Intern", Department: "Sales"}, true},
		{"legal department", jobs.RawJob{Title: "Summer Intern", Department: "Legal"}, false},
		{"recruiting department", jobs.RawJob{Title: "Intern", Department: "Recruiting"}, false},
		{"marketing department", jobs.RawJob{Title: "Intern", Department: "Marketing"}, false},
		{"finance department", jobs.RawJob{Title: "Intern", Department: "Finance & Accounting"}, false},
		{"engineering department", jobs.RawJob{Title: "Intern", Department: "Engineering"}, true},
		{"research department", jobs.RawJob{Title: "Research Intern", Department: "Research"}, true},
		// Real Databricks postings (benchmark capture, 2026-11-22): the intern is filed under a university-recruiting programme.
		{"engineering intern in a university recruiting programme", jobs.RawJob{Title: "Software Engineering Intern (2027)", Department: "University Recruiting - Engineering"}, true},
		{"gen ai research intern in a university recruiting programme", jobs.RawJob{Title: "PhD GenAI Research Scientist Intern", Department: "University Recruiting - Research"}, true},
		// Real Datadog posting: a staff role that mentions the new graduates its team is made of.
		{"staff engineer whose text mentions university graduates", jobs.RawJob{Title: "Staff Software Engineer - Frontend", Department: "Dev Eng", Description: "We hire at various levels, from new university graduates to senior engineers with years of industry experience."}, false},
		{"a senior title that names new grads is still early-career by its title", jobs.RawJob{Title: "Senior New Grad Program Engineer"}, true},
		{"a university recruiter is still a recruiter", jobs.RawJob{Title: "University Recruiter", Department: "University Recruiting"}, false},
		{"recruiting coordinator intern in a university programme", jobs.RawJob{Title: "Recruiting Coordinator Intern", Department: "University Recruiting"}, false},
	}
	for _, c := range cases {
		if got := IsEarlyCareer(c.job); got != c.want {
			t.Errorf("%s: got %v (%q) want %v", c.name, got, EarlyCareerReason(c.job), c.want)
		}
	}
}

func TestEarlyCareerReasonNamesTheWords(t *testing.T) {
	if r := EarlyCareerReason(jobs.RawJob{Title: "Intern - Frontend"}); r != "title: Intern" {
		t.Errorf("reason %q", r)
	}
	if r := EarlyCareerReason(jobs.RawJob{Title: "Engineer", EmploymentType: "Co-op"}); r != "employment type: Co-op" {
		t.Errorf("reason %q", r)
	}
	if r := EarlyCareerReason(jobs.RawJob{Title: "Engineer", Description: "Open to the class of 2027."}); r != "description: class of 2027" {
		t.Errorf("reason %q", r)
	}
}

func TestFilterEarlyCareerKeepsOrderAndDropsTheRest(t *testing.T) {
	in := []jobs.RawJob{{Title: "Staff Engineer"}, {Title: "Intern - Frontend"}, {Title: "Internal Tools Engineer"}, {Title: "Research Intern"}}
	out := FilterEarlyCareer(in)
	if len(out) != 2 || out[0].Title != "Intern - Frontend" || out[1].Title != "Research Intern" {
		t.Fatalf("%+v", out)
	}
}
