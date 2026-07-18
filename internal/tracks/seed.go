package tracks

const gt7InfoCourseCSVURL = "https://raw.githubusercontent.com/ddm999/gt7info/web-new/_data/db/course.csv"
const gt7TracklistAssetURL = "https://www.gran-turismo.com/common/dist/gt7/tracklist/assets/tracks.gb-DLTcO0kl.js"

func OfficialGT7SeedCatalog() Catalog {
	gt7Info := gt7InfoCourseCSVSource()
	officialTracklist := officialTracklistAssetSource(gt7TracklistAssetURL)

	return Catalog{
		CatalogVersion:   CatalogVersionV1,
		GeneratedBy:      "telemetry-one curated GT7 tracklist asset seed",
		Notes:            "Curated GT7 layout metadata from the official Gran Turismo tracklist asset, cross-checked against gt7info course.csv. Corners are generated as ordinal Corner 1..N entries from NumCorners only; sectors and centerLine are intentionally empty because neither source provides reliable sector boundaries or sampled layout geometry.",
		ApprovedGeometry: defaultApprovedGeometryManifest(),
		Tracks: []CatalogTrack{
			track("gt7_watkins_glen_international", "Watkins Glen International", "United States", []Source{officialTracklist, gt7Info},
				layoutFromGT7Info(1240, "Watkins Glen Long Course", 5423, 11, []Source{officialTracklist, gt7Info}),
				layoutFromGT7Info(1264, "Watkins Glen Short Course", 3942, 7, []Source{gt7Info}),
			),
			track("gt7_tsukuba_circuit", "Tsukuba Circuit", "Japan", []Source{officialTracklist, gt7Info},
				layoutFromGT7Info(471, "Tsukuba Circuit", 2045, 8, []Source{officialTracklist, gt7Info}),
			),
			track("gt7_yas_marina_circuit", "Yas Marina Circuit", "United Arab Emirates", []Source{officialTracklist, gt7Info},
				layoutFromGT7Info(2003, "Yas Marina Circuit", 5281, 16, []Source{officialTracklist, gt7Info}),
			),
			track("gt7_circuit_gilles_villeneuve", "Circuit Gilles-Villeneuve", "Canada", []Source{officialTracklist, gt7Info},
				layoutFromGT7Info(2004, "Circuit Gilles-Villeneuve", 4361, 14, []Source{officialTracklist, gt7Info}),
			),
			track("gt7_weathertech_raceway_laguna_seca", "WeatherTech Raceway Laguna Seca", "United States", []Source{gt7Info},
				layoutFromGT7Info(41, "WeatherTech Raceway Laguna Seca", 3602, 11, []Source{gt7Info}),
			),
			track("gt7_nurburgring", "Nurburgring", "Germany", []Source{gt7Info},
				layoutFromGT7Info(95, "Nurburgring Nordschleife", 20832, 73, []Source{gt7Info}),
				layoutFromGT7Info(349, "Nurburgring GP", 5148, 17, []Source{gt7Info}),
				layoutFromGT7Info(1247, "Nurburgring Endurance", 23864, 85, []Source{gt7Info}),
				layoutFromGT7Info(1248, "Nurburgring Sprint", 3629, 12, []Source{gt7Info}),
			),
			track("gt7_autodrome_lago_maggiore", "Autodrome Lago Maggiore", "Italy", []Source{gt7Info},
				layoutFromGT7Info(365, "Autodrome Lago Maggiore - Full Course", 5809, 17, []Source{gt7Info}),
				layoutFromGT7Info(1226, "Autodrome Lago Maggiore - East", 3643, 11, []Source{gt7Info}),
				layoutFromGT7Info(1231, "Autodrome Lago Maggiore - East Reverse", 3643, 11, []Source{gt7Info}),
			),
			track("gt7_lake_louise", "Lake Louise", "Canada", []Source{gt7Info},
				layoutFromGT7Info(1309, "Lake Louise Long Track", 3694, 11, []Source{gt7Info}),
				layoutFromGT7Info(1318, "Lake Louise Long Track Reverse", 3694, 11, []Source{gt7Info}),
			),
			track("gt7_suzuka_circuit", "Suzuka Circuit", "Japan", []Source{gt7Info},
				layoutFromGT7Info(10, "Suzuka Circuit", 5807, 20, []Source{gt7Info}),
				layoutFromGT7Info(442, "Suzuka Circuit East Course", 2243, 9, []Source{gt7Info}),
			),
			track("gt7_daytona_international_speedway", "Daytona International Speedway", "United States", []Source{gt7Info},
				layoutFromGT7Info(4, "Daytona Tri-Oval", 4023, 4, []Source{gt7Info}),
				layoutFromGT7Info(1163, "Daytona Road Course", 5729, 12, []Source{gt7Info}),
			),
			track("gt7_circuit_de_spa_francorchamps", "Circuit de Spa-Francorchamps", "Belgium", []Source{gt7Info},
				layoutFromGT7Info(462, "Circuit de Spa-Francorchamps", 7004, 21, []Source{gt7Info}),
				layoutFromGT7Info(1269, "Spa 24h Layout", 7004, 21, []Source{gt7Info}),
			),
			track("gt7_autodromo_nazionale_monza", "Autodromo Nazionale Monza", "Italy", []Source{gt7Info},
				layoutFromGT7Info(469, "Autodromo Nazionale Monza", 5793, 11, []Source{gt7Info}),
				layoutFromGT7Info(742, "Autodromo Nazionale Monza No Chicane", 5755, 9, []Source{gt7Info}),
			),
			track("gt7_trial_mountain_circuit", "Trial Mountain Circuit", "United States", []Source{gt7Info},
				layoutFromGT7Info(1024, "Trial Mountain Circuit", 5434, 15, []Source{gt7Info}),
				layoutFromGT7Info(1034, "Trial Mountain Circuit - Reverse", 5434, 15, []Source{gt7Info}),
			),
			track("gt7_michelin_raceway_road_atlanta", "Michelin Raceway Road Atlanta", "United States", []Source{gt7Info},
				layoutFromGT7Info(1007, "Michelin Raceway Road Atlanta", 4088, 12, []Source{gt7Info}),
			),
			track("gt7_autodromo_de_interlagos", "Autodromo de Interlagos", "Brazil", []Source{gt7Info},
				layoutFromGT7Info(152, "Autodromo de Interlagos", 4309, 15, []Source{gt7Info}),
			),
			track("gt7_red_bull_ring", "Red Bull Ring", "Austria", []Source{gt7Info},
				layoutFromGT7Info(470, "Red Bull Ring", 4318, 10, []Source{gt7Info}),
				layoutFromGT7Info(846, "Red Bull Ring Short Track", 2336, 6, []Source{gt7Info}),
			),
			track("gt7_circuit_de_barcelona_catalunya", "Circuit de Barcelona-Catalunya", "Spain", []Source{gt7Info},
				layoutFromGT7Info(874, "Circuit de Barcelona-Catalunya Grand Prix Layout", 4655, 16, []Source{gt7Info}),
				layoutFromGT7Info(1249, "Circuit de Barcelona-Catalunya GP Layout No Chicane", 4730, 14, []Source{gt7Info}),
				layoutFromGT7Info(1250, "Circuit de Barcelona-Catalunya National", 2977, 11, []Source{gt7Info}),
			),
			track("gt7_24_heures_du_mans", "24 Heures du Mans Racing Circuit", "France", []Source{gt7Info},
				layoutFromGT7Info(454, "24 Heures du Mans Racing Circuit", 13629, 38, []Source{gt7Info}),
				layoutFromGT7Info(854, "24 Heures du Mans Racing Circuit No Chicane", 13567, 32, []Source{gt7Info}),
			),
			track("gt7_fuji_international_speedway", "Fuji International Speedway", "Japan", []Source{gt7Info},
				layoutFromGT7Info(16, "Fuji International Speedway", 4563, 16, []Source{gt7Info}),
				layoutFromGT7Info(837, "Fuji International Speedway (Short)", 4526, 14, []Source{gt7Info}),
			),
		},
	}
}

func track(id string, name string, country string, sources []Source, layouts ...CatalogLayout) CatalogTrack {
	return CatalogTrack{ID: id, Name: name, Country: country, Sources: sources, Layouts: layouts}
}

func layoutFromGT7Info(gt7LayoutID uint, name string, lengthMeters float64, numCorners uint, sources []Source) CatalogLayout {
	return CatalogLayout{
		ID:           layoutID(gt7LayoutID),
		Name:         name,
		LengthMeters: lengthMeters,
		Sources:      sources,
		Sectors:      []Sector{},
		Corners:      enumeratedCorners(gt7LayoutID, numCorners, sources),
		CenterLine:   []Point{},
	}
}

func layoutID(gt7LayoutID uint) string {
	return "gt7_layout_" + itoa(gt7LayoutID)
}

func enumeratedCorners(gt7LayoutID uint, numCorners uint, sources []Source) []Corner {
	if numCorners == 0 {
		return []Corner{}
	}
	corners := make([]Corner, 0, numCorners)
	for i := uint(1); i <= numCorners; i++ {
		corners = append(corners, Corner{
			ID:             layoutID(gt7LayoutID) + "_corner_" + itoa(i),
			Number:         int(i),
			Name:           "Corner " + itoa(i),
			DefinitionMode: CornerDefinitionCatalogEnumerated,
			Confidence:     1,
			Sources:        sources,
		})
	}

	return corners
}

func gt7InfoCourseCSVSource() Source {
	return Source{
		URL:         gt7InfoCourseCSVURL,
		SourceType:  SourceTypeGT7InfoCourseCSV,
		RetrievedAt: "2026-07-15",
		Note:        "gt7info course.csv fields: ID, Name, Base, Country, Category, Length, IsReverse, IsOval, NumCorners; NumCorners is used only to generate ordinal Corner 1..N labels, not named/ranged corners",
	}
}

func officialTracklistAssetSource(url string) Source {
	return Source{
		URL:         url,
		SourceType:  SourceTypeOfficialGranTurismoTracklistAsset,
		RetrievedAt: "2026-07-12",
		Note:        "official Gran Turismo generated tracklist asset; public first-party source for layout ID, name, length, and cornerCount",
	}
}

func officialNewsSource(url string) Source {
	return Source{
		URL:         url,
		SourceType:  SourceTypeOfficialGranTurismoNews,
		RetrievedAt: "2026-07-12",
		Note:        "official Gran Turismo news article",
	}
}

func itoa(value uint) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for value > 0 {
		i--
		digits[i] = byte('0' + value%10)
		value /= 10
	}

	return string(digits[i:])
}
