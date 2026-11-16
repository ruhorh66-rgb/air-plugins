package main

import (
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
	var rows []map[string]any
	if json.Unmarshal(res.Data, &rows) != nil {
		return res
	}
	changed := false
	for _, row := range rows {
		if jsonString(row["schema"]) == "air.learning.verdict/v1" && jsonString(row["status"]) == "unjudged" &&
			jsonString(row["reason"]) == "judge not configured" {
			row["note"] = unconfiguredJudgeNote
			changed = true
		}
	}
	if !changed {
		return res
	}
	if b, err := json.Marshal(rows); err == nil {
		res.Data = b
	}
	return res
}
