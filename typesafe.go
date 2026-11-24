package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// typesafe is the TypeSafe System One API.
// https://docs.typesafe.ai/api/system-one
var typesafe = provider{
	keyEnv: "TYPESAFE_API_KEY",
	model:  "jev-latest",
	url:    func() string { return "https://api.typesafe.ai/v1/systemone" },
	request: func(in input, q question) (map[string]any, error) {
		tq := map[string]any{"type": "noul", "instructions": q.text}
		switch {
		case q.choices != nil:
			criteria := map[string]any{}
			for _, o := range q.choices {
				if o.desc != "" {
					criteria[o.name] = o.desc
				} else {
					criteria[o.name] = nil
				}
			}
			tq["type"], tq["criteria"] = "choice", criteria
		case q.levels != nil:
			tq["type"], tq["criteria"] = "score", q.levels
		case q.box:
			tq["type"] = "box"
		}
		req := map[string]any{"questions": map[string]any{"q": tq}}
		assets := map[string]any{}
		req["state"] = typesafeState(in, assets)
		if len(assets) > 0 {
			req["assets"] = assets
		}
		return req, nil
	},
	answer: func(body []byte, _ question) (answer, error) {
		var res struct {
			Answers struct{ Q answer } `json:"answers"`
		}
		err := json.Unmarshal(body, &res)
		return res.Answers.Q, err
	},
	// Images and box questions are off by default. This header turns them on.
	// https://docs.typesafe.ai/images#turning-on-images
	finish: func(req map[string]any, h http.Header) {
		_, images := req["assets"]
		questions, _ := req["questions"].(map[string]any)
		for _, tq := range questions {
			if m, _ := tq.(map[string]any); m["type"] == "box" {
				images = true
			}
		}
		if images {
			h.Set("TypeSafe-Danger-Zone-Images", "true")
		}
	},
}

// typesafeState builds the state and adds one asset for each image. The state
// is the text when there are no images. It is the image reference when the
// input is one image and no text. In all other cases it is an object with the
// keys "text" and "images".
func typesafeState(in input, assets map[string]any) any {
	var refs []string
	for _, img := range in.images {
		// The API replaces each occurrence of a reference in the request with
		// the image. Thus the reference must not occur in the text by accident.
		b := make([]byte, 16)
		rand.Read(b)
		ref := "image-" + hex.EncodeToString(b)
		refs = append(refs, ref)
		assets[ref] = map[string]any{
			"type":        "image",
			"format":      img.format,
			"data_base64": base64.StdEncoding.EncodeToString(img.data),
		}
	}
	hasText := strings.TrimSpace(in.text) != ""
	switch {
	case len(refs) == 0:
		return in.text
	case len(refs) == 1 && !hasText:
		return refs[0]
	}
	state := map[string]any{"images": refs}
	if hasText {
		state["text"] = in.text
	}
	return state
}
