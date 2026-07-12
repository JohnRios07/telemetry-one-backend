package tracks

func OfficialGT7SeedCatalog() Catalog {
	return Catalog{
		CatalogVersion: CatalogVersionV1,
		GeneratedBy:    "telemetry-one manual official-source seed",
		Notes:          "Initial GT7 seed curated only from official Gran Turismo URLs. Corners and sectors are intentionally empty because the cited official pages do not provide corner-name catalogs or sector boundaries.",
		Tracks: []CatalogTrack{
			{
				ID:      "gt7_watkins_glen_international",
				Name:    "Watkins Glen International",
				Country: "United States",
				Sources: []Source{officialNewsSource("https://www.gran-turismo.com/us/news/00_5302315.html")},
				Layouts: []CatalogLayout{
					{
						ID:           "gt7_watkins_glen_long_course",
						Name:         "Watkins Glen Long Course",
						LengthMeters: 5423,
						Sources:      []Source{officialNewsSource("https://www.gran-turismo.com/us/news/00_5302315.html")},
						Sectors:      []Sector{},
						Corners:      []Corner{},
					},
				},
			},
			{
				ID:      "gt7_yas_marina_circuit",
				Name:    "Yas Marina Circuit",
				Country: "United Arab Emirates",
				Sources: []Source{officialNewsSource("https://www.gran-turismo.com/us/news/00_4185758.html")},
				Layouts: []CatalogLayout{
					{
						ID:           "gt7_yas_marina_full_course",
						Name:         "Full Course",
						LengthMeters: 5281,
						Sources:      []Source{officialNewsSource("https://www.gran-turismo.com/us/news/00_4185758.html")},
						Sectors:      []Sector{},
						Corners:      []Corner{},
					},
				},
			},
			{
				ID:      "gt7_circuit_gilles_villeneuve",
				Name:    "Circuit Gilles-Villeneuve",
				Sources: []Source{officialNewsSource("https://www.gran-turismo.com/us/news/00_4185758.html")},
				Layouts: []CatalogLayout{
					{
						ID:           "gt7_circuit_gilles_villeneuve_full_course",
						Name:         "Full Course",
						LengthMeters: 4361,
						Sources:      []Source{officialNewsSource("https://www.gran-turismo.com/us/news/00_4185758.html")},
						Sectors:      []Sector{},
						Corners:      []Corner{},
					},
				},
			},
		},
	}
}

func officialNewsSource(url string) Source {
	return Source{
		URL:         url,
		SourceType:  "official_gran_turismo_news",
		RetrievedAt: "2026-07-12",
		Note:        "official Gran Turismo source",
	}
}
