package ats

// Adapters returns one adapter per provider name, all sharing the fetcher f (and so its per-host throttle). A nil
// f gets a default Fetcher.
func Adapters(f *Fetcher) map[string]Adapter {
	f = orDefault(f)
	list := []Adapter{
		Greenhouse{F: f},
		Lever{F: f},
		Lever{F: f, EU: true},
		Ashby{F: f},
		SmartRecruiters{F: f},
		Workday{F: f},
		Recruitee{F: f},
		Workable{F: f},
		CareersJSONLD{F: f},
		Internshala{F: f},
		Unstop{F: f},
		InternDock{F: f},
	}
	m := make(map[string]Adapter, len(list))
	for _, a := range list {
		m[a.Provider()] = a
	}
	return m
}
