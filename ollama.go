package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
)

// ollama is the chat API of a local Ollama server. Ollama has no decision API.
// Thus the program gives each possible answer a label of one token and tells
// the model to reply with one label. The probability of each answer comes from
// the log probabilities of the labels at the position of the reply.
// https://docs.ollama.com/api/chat
var ollama = provider{
	url: func() string {
		host := os.Getenv("OLLAMA_HOST")
		if host == "" {
			host = "localhost:11434"
		}
		if !strings.Contains(host, "://") {
			host = "http://" + host
		}
		return strings.TrimRight(host, "/") + "/api/chat"
	},
	request: func(in input, q question) (map[string]any, error) {
		labels, lines, err := ollamaLabels(q)
		if err != nil {
			return nil, err
		}
		var prompt strings.Builder
		if strings.TrimSpace(in.text) != "" {
			fmt.Fprintf(&prompt, "Input:\n%s\n\n", in.text)
		}
		if len(in.images) > 0 {
			prompt.WriteString("The attached images are part of the input.\n\n")
		}
		fmt.Fprintf(&prompt, "Question: %s\n\nLabels:\n", q.text)
		for i, label := range labels {
			fmt.Fprintf(&prompt, "%s%s\n", label, lines[i])
		}
		user := map[string]any{"role": "user", "content": prompt.String()}
		if len(in.images) > 0 {
			var images []string
			for _, img := range in.images {
				images = append(images, base64.StdEncoding.EncodeToString(img.data))
			}
			user["images"] = images
		}
		return map[string]any{
			"messages": []any{
				map[string]any{"role": "system", "content": "You answer one question about the input. Reply with only the label of your answer. Do not add other text."},
				user,
			},
			"think":        false,
			"logprobs":     true,
			"top_logprobs": 20,
			"options":      map[string]any{"temperature": 0, "num_predict": 4096},
		}, nil
	},
	answer: func(body []byte, q question) (answer, error) {
		labels, _, err := ollamaLabels(q)
		if err != nil {
			return answer{}, err
		}
		p, err := ollamaProbabilities(body, labels)
		if err != nil {
			return answer{}, err
		}
		switch {
		case q.choices != nil:
			best := 0
			for i := range p {
				if p[i] > p[best] {
					best = i
				}
			}
			return answer{Type: "choice", Choice: q.choices[best].name}, nil
		case q.levels != nil:
			score := 0.0
			for i := range p {
				score += float64(i) * p[i]
			}
			return answer{Type: "score", Score: score}, nil
		}
		return answer{Type: "noul", Noul: p[0]}, nil
	},
	// The default of the chat API is a stream of JSON lines.
	finish: func(req map[string]any, _ http.Header) {
		if _, ok := req["stream"]; !ok {
			req["stream"] = false
		}
	},
}

// ollamaLabels returns one label for each possible answer, and the text that
// the prompt shows after each label. Each label is one token in common
// tokenizers: "yes" and "no", one letter for each choice, or one digit for
// each level.
func ollamaLabels(q question) (labels, lines []string, err error) {
	switch {
	case q.box:
		return nil, nil, fmt.Errorf("provider ollama has no box questions")
	case q.choices != nil:
		if len(q.choices) > 26 {
			return nil, nil, fmt.Errorf("provider ollama accepts at most 26 choices, got %d", len(q.choices))
		}
		for i, o := range q.choices {
			labels = append(labels, string(rune('A'+i)))
			line := ": " + o.name
			if o.desc != "" {
				line += " - " + o.desc
			}
			lines = append(lines, line)
		}
	case q.levels != nil:
		if len(q.levels) > 10 {
			return nil, nil, fmt.Errorf("provider ollama accepts at most 10 levels, got %d", len(q.levels))
		}
		for i, l := range q.levels {
			labels = append(labels, string(rune('0'+i)))
			lines = append(lines, ": "+l)
		}
	default:
		labels, lines = []string{"yes", "no"}, []string{"", ""}
	}
	return labels, lines, nil
}

// ollamaProbabilities returns the probability of each label. It finds the
// token of the reply in the log probabilities. Then it adds the probabilities
// of the alternative tokens for each label, and scales the sum to 1. A model
// that thinks emits its thoughts before the reply. Thus the reply is the last
// token that is equal to the content.
func ollamaProbabilities(body []byte, labels []string) ([]float64, error) {
	var res struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Logprobs []struct {
			Token       string `json:"token"`
			TopLogprobs []struct {
				Token   string  `json:"token"`
				Logprob float64 `json:"logprob"`
			} `json:"top_logprobs"`
		} `json:"logprobs"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}
	index := func(token string) int {
		for i, label := range labels {
			if strings.EqualFold(strings.TrimSpace(token), label) {
				return i
			}
		}
		return -1
	}
	reply := index(res.Message.Content)
	if reply < 0 {
		return nil, fmt.Errorf("the model replied %q, not one of the labels %s", res.Message.Content, strings.Join(labels, ", "))
	}
	p := make([]float64, len(labels))
	for i := len(res.Logprobs) - 1; i >= 0; i-- {
		if index(res.Logprobs[i].Token) != reply {
			continue
		}
		sum := 0.0
		for _, top := range res.Logprobs[i].TopLogprobs {
			if j := index(top.Token); j >= 0 {
				p[j] += math.Exp(top.Logprob)
				sum += math.Exp(top.Logprob)
			}
		}
		if sum == 0 {
			break
		}
		for j := range p {
			p[j] /= sum
		}
		return p, nil
	}
	return nil, fmt.Errorf("the response has no log probabilities for the reply %q: the Ollama server is possibly too old", res.Message.Content)
}
