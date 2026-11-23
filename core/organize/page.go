package organize

// PlanPage is a plan as the Organize dialog shows it (Organize's, and Promote's, #171): its
// counts, and a page of each list.
type PlanPage struct {
	Token string `json:"token"`
	// Tracks and Files count the moves of Tracks, and of their albums' other Files.
	Tracks   int            `json:"tracks"`
	Files    int            `json:"files"`
	InPlace  int            `json:"inPlace"`
	Unplaced map[string]int `json:"unplaced"`
	Moves    []Row          `json:"moves"`
	Left     []Row          `json:"left"`
	// TotalMoves and TotalLeft are the lengths of the two lists, which Moves and Left page.
	TotalMoves int `json:"totalMoves"`
	TotalLeft  int `json:"totalLeft"`
}

// Page is the plan's counts, with limit rows of each list from start.
func (p *Plan) Page(start, limit int) PlanPage {
	res := PlanPage{Token: p.Token, InPlace: p.InPlace, Unplaced: map[string]int{},
		Moves: page(p.Moves, start, limit), Left: page(p.Unplaced, start, limit),
		TotalMoves: len(p.Moves), TotalLeft: len(p.Unplaced)}
	for _, m := range p.Moves {
		if m.Status == StatusFile {
			res.Files++
		} else {
			res.Tracks++
		}
	}
	for _, u := range p.Unplaced {
		res.Unplaced[u.Status]++
	}
	return res
}

func page(rows []Row, start, limit int) []Row {
	start = min(max(start, 0), len(rows))
	return rows[start:min(start+max(limit, 0), len(rows))]
}
