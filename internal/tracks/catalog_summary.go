package tracks

type CatalogTrackLayoutSummary struct {
	CatalogVersion string                   `json:"catalogVersion"`
	Tracks         []CatalogTrackLayoutItem `json:"tracks"`
}

type CatalogTrackLayoutItem struct {
	ID      string                          `json:"id"`
	Name    string                          `json:"name"`
	Country string                          `json:"country,omitempty"`
	Sources []Source                        `json:"sources,omitempty"`
	Layouts []CatalogTrackLayoutSummaryItem `json:"layouts"`
}

type CatalogTrackLayoutSummaryItem struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	LengthMeters float64  `json:"lengthMeters"`
	Sources      []Source `json:"sources,omitempty"`
}

func BuildCatalogTrackLayoutSummary(catalog Catalog) CatalogTrackLayoutSummary {
	result := CatalogTrackLayoutSummary{CatalogVersion: catalog.CatalogVersion}
	for _, track := range catalog.Tracks {
		trackSources := filterProductionSources(track.Sources)
		if len(trackSources) == 0 {
			continue
		}

		item := CatalogTrackLayoutItem{
			ID:      track.ID,
			Name:    track.Name,
			Country: track.Country,
			Sources: trackSources,
		}

		for _, layout := range track.Layouts {
			if !hasSelectableLayoutMetadata(track, layout) {
				continue
			}

			layoutSources := filterProductionSources(layout.Sources)
			if len(layoutSources) == 0 {
				continue
			}

			item.Layouts = append(item.Layouts, CatalogTrackLayoutSummaryItem{
				ID:           layout.ID,
				Name:         layout.Name,
				LengthMeters: layout.LengthMeters,
				Sources:      layoutSources,
			})
		}

		if len(item.Layouts) == 0 {
			continue
		}

		result.Tracks = append(result.Tracks, item)
	}

	return result
}

func FindSelectableTrackLayout(catalog Catalog, trackID, layoutID string) (CatalogTrack, CatalogLayout, bool) {
	for _, track := range catalog.Tracks {
		if track.ID != trackID {
			continue
		}
		if len(filterProductionSources(track.Sources)) == 0 {
			return CatalogTrack{}, CatalogLayout{}, false
		}
		for _, layout := range track.Layouts {
			if layout.ID != layoutID {
				continue
			}
			if len(filterProductionSources(layout.Sources)) == 0 {
				return CatalogTrack{}, CatalogLayout{}, false
			}
			if len(track.Sources) == 0 || len(layout.Sources) == 0 {
				return CatalogTrack{}, CatalogLayout{}, false
			}
			if validateSources("track.sources", track.Sources) != nil || validateSources("layout.sources", layout.Sources) != nil {
				return CatalogTrack{}, CatalogLayout{}, false
			}

			return track, layout, true
		}

		return CatalogTrack{}, CatalogLayout{}, false
	}

	return CatalogTrack{}, CatalogLayout{}, false
}

func filterProductionSources(sources []Source) []Source {
	filtered := make([]Source, 0, len(sources))
	for _, source := range sources {
		if !isProductionSourceType(source.SourceType) {
			continue
		}
		filtered = append(filtered, source)
	}

	return filtered
}
