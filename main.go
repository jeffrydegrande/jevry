// jevry reads input from the files named in argv, or from stdin when argv is
// empty (like Ruby's ARGF.read), and sends it to a decision model. The
// --provider flag selects the API: typesafe (Jev, the default) or openai
// (the Decisions API).
//
// Without --question, the input is a full request body for the provider. The
// program prints the raw response.
//
// With --question, the input is the state. The question type depends on the
// other flags:
//   - no other flags: yes/no. Prints the probability of yes.
//   - --choice (repeatable): choice. Prints the chosen option.
//   - --level (repeatable): score. Prints the score, from 0 to len(levels)-1.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// question is one question in a provider-neutral form. With no choices and no
// levels, it is a yes/no question.
type question struct {
	text    string
	choices []option
	levels  []string
}

type option struct{ name, desc string }

// answer is the parsed answer to a question. Type is "noul", "choice" or
// "score". Noul is the probability of yes.
type answer struct {
	Type   string
	Noul   float64
	Choice any
	Score  float64
}

type provider struct {
	url, keyEnv, model string
	request            func(state string, q question) map[string]any
	answer             func(body []byte) (answer, error)
}

var providers = map[string]provider{
	// https://docs.typesafe.ai/api
	"typesafe": {
		url:    "https://api.typesafe.ai/v1/systemone",
		keyEnv: "TYPESAFE_API_KEY",
		model:  "jev-latest",
		request: func(state string, q question) map[string]any {
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
				tq = map[string]any{"type": "choice", "instructions": q.text, "criteria": criteria}
			case q.levels != nil:
				tq = map[string]any{"type": "score", "instructions": q.text, "criteria": q.levels}
			}
			return map[string]any{"state": state, "questions": map[string]any{"q": tq}}
		},
		answer: func(body []byte) (answer, error) {
			var res struct {
				Answers struct{ Q answer } `json:"answers"`
			}
			err := json.Unmarshal(body, &res)
			return res.Answers.Q, err
		},
	},
	// https://developers.openai.com/api/docs/guides/decisions
	"openai": {
		url:    "https://api.openai.com/v1/decisions",
		keyEnv: "OPENAI_API_KEY",
		model:  "gpt-6-luna",
		request: func(state string, q question) map[string]any {
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
				oq = map[string]any{"type": "choice", "name": "q", "instructions": q.text, "choices": choices}
			case q.levels != nil:
				var levels []map[string]any
				for _, l := range q.levels {
					levels = append(levels, map[string]any{"label": l})
				}
				oq = map[string]any{"type": "score", "name": "q", "instructions": q.text, "levels": levels}
			}
			return map[string]any{"input": state, "questions": []any{oq}}
		},
		answer: func(body []byte) (answer, error) {
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
	},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "jevry:", err)
		os.Exit(1)
	}
}

func run() error {
	providerName := flag.String("provider", "typesafe", "API to use: typesafe or openai")
	text := flag.String("question", "", "question to ask about the input (yes/no unless --choice or --level is set)")
	var q question
	flag.Func("choice", "option for a choice question, as `name` or name=description (repeatable)", func(s string) error {
		name, desc, _ := strings.Cut(s, "=")
		q.choices = append(q.choices, option{name, desc})
		return nil
	})
	flag.Func("level", "level description for a score question, lowest first (repeatable)", func(s string) error {
		q.levels = append(q.levels, s)
		return nil
	})
	flag.Parse()
	q.text = *text

	p, ok := providers[*providerName]
	if !ok {
		return fmt.Errorf("unknown provider %q (use typesafe or openai)", *providerName)
	}
	switch {
	case q.choices != nil && q.levels != nil:
		return fmt.Errorf("--choice and --level cannot be used together")
	case (q.choices != nil || q.levels != nil) && q.text == "":
		return fmt.Errorf("--choice and --level need --question")
	}

	in, err := readARGF(flag.Args())
	if err != nil {
		return err
	}

	var req map[string]any
	if q.text != "" {
		req = p.request(string(in), q)
	} else if err := json.Unmarshal(in, &req); err != nil {
		return fmt.Errorf("input is not a JSON object: %w", err)
	}
	if _, ok := req["model"]; !ok {
		req["model"] = p.model
	}
	body, _ := json.Marshal(req)

	key := os.Getenv(p.keyEnv)
	if key == "" {
		return fmt.Errorf("%s is not set", p.keyEnv)
	}

	httpReq, _ := http.NewRequest("POST", p.url, bytes.NewReader(body))
	httpReq.Header.Set("Authorization", "Bearer "+key)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		os.Stdout.Write(out)
		fmt.Println()
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	if q.text == "" {
		os.Stdout.Write(out)
		fmt.Println()
		return nil
	}

	a, err := p.answer(out)
	if err != nil {
		return err
	}
	switch a.Type {
	case "noul":
		fmt.Printf("%.2f\n", a.Noul)
	case "choice":
		fmt.Println(a.Choice)
	case "score":
		fmt.Printf("%.2f\n", a.Score)
	default:
		return fmt.Errorf("unexpected answer type %q", a.Type)
	}
	return nil
}

// readARGF concatenates the named files, or reads stdin when there are none.
// A name of "-" also means stdin.
func readARGF(names []string) ([]byte, error) {
	if len(names) == 0 {
		return io.ReadAll(os.Stdin)
	}
	var buf bytes.Buffer
	for _, name := range names {
		var b []byte
		var err error
		if name == "-" {
			b, err = io.ReadAll(os.Stdin)
		} else {
			b, err = os.ReadFile(name)
		}
		if err != nil {
			return nil, err
		}
		buf.Write(b)
	}
	return buf.Bytes(), nil
}
