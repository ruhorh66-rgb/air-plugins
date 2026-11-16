package main

import (
	"bytes"
	"encoding/json"

	"github.com/ruhorh66-rgb/air-modules/learning"
)

// The CLI states what an unconfigured judge means, so no learned procedure is
// needed for it (A3.3): 32 duplicate procedures taught exactly this sentence.
const unconfiguredJudgeNote = "судья не настроен → прогон не оценён"

func learningJudgeStatus(s sharedLearningSettings) map[string]any {
	if s.Judge != nil {
		return map[string]any{"configured": true}
	}
	return map[string]any{"configured": false, "note": unconfiguredJudgeNote}
}

// annotateUnconfiguredJudgeVerdicts adds the note to each verdict row of a
// `learn events` answer that air-modules wrote as "unjudged / judge not
// configured". Rows are otherwise returned unchanged; an answer of another
// shape is passed through as is.
func annotateUnconfiguredJudgeVerdicts(res learning.Response) learning.Response {
	// Rows stay raw: only a matching verdict row is rebuilt, every other row
	// keeps its exact bytes (numbers, key order, duplicates).
	var rows []json.RawMessage
	if json.Unmarshal(res.Data, &rows) != nil {
		return res
	}
	changed := false
	for i, raw := range rows {
		var row map[string]any
		if json.Unmarshal(raw, &row) != nil || jsonString(row["schema"]) != "air.learning.verdict/v1" ||
			jsonString(row["status"]) != "unjudged" || jsonString(row["reason"]) != "judge not configured" {
			continue
		}
		row["note"] = unconfiguredJudgeNote
		if b, err := json.Marshal(row); err == nil {
			rows[i] = b
			changed = true
		}
	}
	if !changed {
		return res
	}
	// Joined by hand: json.Marshal would compact every raw row.
	parts := make([][]byte, len(rows))
	for i := range rows {
		parts[i] = rows[i]
	}
	res.Data = append(append([]byte("["), bytes.Join(parts, []byte(","))...), ']')
	return res
}
