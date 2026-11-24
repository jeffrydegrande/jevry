package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// openai is the OpenAI Decisions API.
// https://developers.openai.com/api/docs/guides/decisions
var openai = provider{
	keyEnv: "OPENAI_API_KEY",
	model:  "gpt-6-luna",
	url:    func() string { return "https://api.openai.com/v1/decisions" },
	request: func(in input, q question) (map[string]any, error) {
		oq := map[string]any{"type": "predicate", "name": "q", "instructions": q.text}
		switch {
		case q.choices != nil:
			var choices []map[string]any
			for _, o := range q.choices {
				c := map[string]any{"value": o.name}
				if o.desc != "" {
					c["description"] = o.desc
				}
				choices = append(choices, c)
			}
			oq["type"], oq["choices"] = "choice", choices
		case q.levels != nil:
			var levels []map[string]any
			for _, l := range q.levels {
				levels = append(levels, map[string]any{"label": l})
			}
			oq["type"], oq["levels"] = "score", levels
		case q.box:
			return nil, fmt.Errorf("provider openai has no box questions")
		}
		return map[string]any{"input": openaiInput(in), "questions": []any{oq}}, nil
	},
	answer: func(body []byte, _ question) (answer, error) {
		var res struct {
			Answers []struct {
				Type        string  `json:"type"`
				Probability float64 `json:"probability"`
				Choice      any     `json:"choice"`
				Score       float64 `json:"score"`
			} `json:"answers"`
		}
		if err := json.Unmarshal(body, &res); err != nil {
			return answer{}, err
		}
		if len(res.Answers) != 1 {
			return answer{}, fmt.Errorf("got %d answers, want 1", len(res.Answers))
		}
		a := res.Answers[0]
		switch a.Type {
		case "refusal":
			return answer{}, fmt.Errorf("the model refused to answer")
		case "predicate":
			a.Type = "noul"
		}
		return answer{Type: a.Type, Noul: a.Probability, Choice: a.Choice, Score: a.Score}, nil
	},
}

// openaiInput returns the text when there are no images. With images, it
// returns one user message with the text and the images as content parts.
func openaiInput(in input) any {
	if len(in.images) == 0 {
		return in.text
	}
	var parts []map[string]any
	if strings.TrimSpace(in.text) != "" {
		parts = append(parts, map[string]any{"type": "input_text", "text": in.text})
	}
	for _, img := range in.images {
		mime := "image/png"
		if img.format == "jpg" {
			mime = "image/jpeg"
		}
		parts = append(parts, map[string]any{
			"type":      "input_image",
			"image_url": "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(img.data),
		})
	}
	return []any{map[string]any{"role": "user", "content": parts}}
}
