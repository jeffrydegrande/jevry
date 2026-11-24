package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// png is the start of a PNG file. It is sufficient for the image detection.
var png = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

// call runs jevry against a fake server that replies with response. It
// returns the output, the request that the server received, and the error.
func call(t *testing.T, name, response, stdin string, args ...string) (string, map[string]any, http.Header, error) {
	t.Helper()
	var body map[string]any
	var header http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &body); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		io.WriteString(w, response)
	}))
	defer srv.Close()

	p := providers[name]
	saved := p
	p.url = func() string { return srv.URL }
	providers[name] = p
	defer func() { providers[name] = saved }()
	if p.keyEnv != "" {
		t.Setenv(p.keyEnv, "test-key")
	}

	var out bytes.Buffer
	err := run(append(args, "--provider", name), strings.NewReader(stdin), &out)
	return out.String(), body, header, err
}

// get returns the value at the path of keys in a decoded JSON value.
func get(t *testing.T, v any, path ...any) any {
	t.Helper()
	for _, key := range path {
		switch k := key.(type) {
		case string:
			v = v.(map[string]any)[k]
		case int:
			v = v.([]any)[k]
		}
	}
	return v
}

func imageFile(t *testing.T) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "photo.png")
	if err := os.WriteFile(name, png, 0o600); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestTypesafe(t *testing.T) {
	tests := []struct {
		name, response, want, wantType string
		args                           []string
	}{
		{"noul", `{"answers":{"q":{"type":"noul","noul":0.938}}}`, "0.94\n", "noul", []string{"noul", "Why?"}},
		{"choice", `{"answers":{"q":{"type":"choice","choice":"billing"}}}`, "billing\n", "choice", []string{"choice", "Why?", "--option", "billing=Payments", "-o", "sales"}},
		{"score", `{"answers":{"q":{"type":"score","score":1.04}}}`, "1.04\n", "score", []string{"score", "Why?", "--level", "Calm", "-l", "Angry"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, body, header, err := call(t, "typesafe", tt.response, "the state", tt.args...)
			if err != nil {
				t.Fatal(err)
			}
			if out != tt.want {
				t.Errorf("output = %q, want %q", out, tt.want)
			}
			if got := get(t, body, "questions", "q", "type"); got != tt.wantType {
				t.Errorf("question type = %v, want %v", got, tt.wantType)
			}
			if body["state"] != "the state" || body["model"] != "jev-latest" {
				t.Errorf("state = %v, model = %v", body["state"], body["model"])
			}
			if header.Get("Authorization") != "Bearer test-key" {
				t.Errorf("Authorization = %q", header.Get("Authorization"))
			}
			if header.Get("TypeSafe-Danger-Zone-Images") != "" {
				t.Error("the images header is set on a request without images")
			}
		})
	}
}

func TestTypesafeBox(t *testing.T) {
	response := `{"answers":{"q":{"type":"box","box":{"top":0.25,"bottom":0.82,"left":0.44,"right":0.84},"confidence":null}}}`
	out, body, header, err := call(t, "typesafe", response, "", "box", "the mug", imageFile(t))
	if err != nil {
		t.Fatal(err)
	}
	if want := "top=0.250 bottom=0.820 left=0.440 right=0.840\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if header.Get("TypeSafe-Danger-Zone-Images") != "true" {
		t.Error("the images header is not set")
	}
	ref, _ := body["state"].(string)
	asset, _ := get(t, body, "assets").(map[string]any)[ref].(map[string]any)
	if asset["format"] != "png" || asset["data_base64"] == "" {
		t.Errorf("state %q has no PNG asset: %v", ref, body["assets"])
	}
}

func TestTypesafeTextAndImages(t *testing.T) {
	_, body, _, err := call(t, "typesafe", `{"answers":{"q":{"type":"noul","noul":1}}}`, "some text", "noul", "Why?", "-", imageFile(t), imageFile(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := get(t, body, "state", "text"); got != "some text" {
		t.Errorf("state.text = %v", got)
	}
	if n := len(get(t, body, "state", "images").([]any)); n != 2 || len(body["assets"].(map[string]any)) != 2 {
		t.Errorf("got %d image references and assets %v, want 2 of each", n, body["assets"])
	}
}

func TestRawMode(t *testing.T) {
	request := `{"model":"jev-1.13.0","state":"s","questions":{"a":{"type":"box"}}}`
	out, body, header, err := call(t, "typesafe", `{"raw":true}`, request, "raw")
	if err != nil {
		t.Fatal(err)
	}
	if out != "{\"raw\":true}\n" {
		t.Errorf("output = %q", out)
	}
	if body["model"] != "jev-1.13.0" {
		t.Errorf("model = %v, want the model of the request", body["model"])
	}
	if header.Get("TypeSafe-Danger-Zone-Images") != "true" {
		t.Error("the images header is not set for a raw box question")
	}

	_, body, _, err = call(t, "ollama", `{}`, `{"model":"m","messages":[]}`, "raw", "--model", "other")
	if err != nil {
		t.Fatal(err)
	}
	if body["model"] != "other" || body["stream"] != false {
		t.Errorf("model = %v, stream = %v", body["model"], body["stream"])
	}
}

func TestOpenAI(t *testing.T) {
	out, body, _, err := call(t, "openai", `{"answers":[{"type":"predicate","name":"q","probability":0.5}]}`, "text", "noul", "Why?", "-", imageFile(t))
	if err != nil {
		t.Fatal(err)
	}
	if out != "0.50\n" {
		t.Errorf("output = %q", out)
	}
	if got := get(t, body, "input", 0, "content", 0, "text"); got != "text" {
		t.Errorf("first content part = %v", got)
	}
	url, _ := get(t, body, "input", 0, "content", 1, "image_url").(string)
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Errorf("image_url = %q", url)
	}

	_, body, _, err = call(t, "openai", `{"answers":[{"type":"refusal","name":"q"}]}`, "text", "noul", "Why?")
	if err == nil || body["input"] != "text" {
		t.Errorf("err = %v, input = %v: want a refusal error and a string input", err, body["input"])
	}
}

// ollamaResponse builds a chat response. The model thinks first. Then it
// replies with the first of the given alternative tokens.
func ollamaResponse(tops ...string) string {
	type top struct {
		Token   string  `json:"token"`
		Logprob float64 `json:"logprob"`
	}
	type entry struct {
		Token string `json:"token"`
		Top   []top  `json:"top_logprobs"`
	}
	logprob := map[string]float64{}
	json.Unmarshal([]byte(tops[0]), &logprob)
	// Each later argument is a token. The first argument maps tokens to
	// log probabilities.
	reply := entry{Token: tops[1]}
	for _, token := range tops[1:] {
		reply.Top = append(reply.Top, top{token, logprob[token]})
	}
	// The thought contains a label. The parser must not use it.
	thought := entry{Token: " no", Top: []top{{" no", 0}}}
	b, _ := json.Marshal(map[string]any{
		"message":  map[string]any{"content": tops[1]},
		"logprobs": []entry{thought, reply},
	})
	return string(b)
}

func TestOllama(t *testing.T) {
	// ln 0.6 = -0.511, ln 0.2 = -1.609, ln 0.1 = -2.303.
	tests := []struct {
		name, response, want string
		args                 []string
	}{
		{"noul", ollamaResponse(`{"yes":-0.511,"Yes":-2.303,"no":-1.609,"maybe":-2.303}`, "yes", "Yes", "no", "maybe"), "0.78\n", []string{"noul", "Why?"}},
		{"choice", ollamaResponse(`{"B":-0.511,"A":-1.609}`, "B", "A"), "sales\n", []string{"choice", "Why?", "-o", "billing=Payments", "-o", "sales"}},
		{"score", ollamaResponse(`{"2":-0.511,"1":-1.609," 0":-1.609}`, "2", "1", " 0"), "1.40\n", []string{"score", "Why?", "-l", "Calm", "-l", "Frustrated", "-l", "Angry"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append(tt.args, "--model", "m")
			out, body, header, err := call(t, "ollama", tt.response, "the state", args...)
			if err != nil {
				t.Fatal(err)
			}
			if out != tt.want {
				t.Errorf("output = %q, want %q", out, tt.want)
			}
			if body["model"] != "m" || body["stream"] != false || body["logprobs"] != true {
				t.Errorf("model = %v, stream = %v, logprobs = %v", body["model"], body["stream"], body["logprobs"])
			}
			if prompt := get(t, body, "messages", 1, "content").(string); !strings.Contains(prompt, "the state") || !strings.Contains(prompt, "Why?") {
				t.Errorf("prompt = %q", prompt)
			}
			if header.Get("Authorization") != "" {
				t.Error("the request has an Authorization header")
			}
		})
	}
}

func TestOllamaImage(t *testing.T) {
	_, body, _, err := call(t, "ollama", ollamaResponse(`{"yes":0}`, "yes"), "", "noul", "Why?", "--model", "m", imageFile(t))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(get(t, body, "messages", 1, "images").([]any)); n != 1 {
		t.Errorf("got %d images, want 1", n)
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name, provider, response, stdin, want string
		args                                  []string
	}{
		{"no question", "typesafe", "", "s", "at least 1 arg", []string{"noul"}},
		{"choice without option", "typesafe", "", "s", `"option" not set`, []string{"choice", "q"}},
		{"score without level", "typesafe", "", "s", `"level" not set`, []string{"score", "q"}},
		{"box without image", "typesafe", "", "s", "needs a PNG or JPEG", []string{"box", "q", "-"}},
		{"raw input is not JSON", "typesafe", "", "s", "not a JSON object", []string{"raw"}},
		{"unknown command", "typesafe", "", "s", "unknown command", []string{"nope"}},
		{"ollama without model", "ollama", "", "s", "set --model", []string{"noul", "q"}},
		{"ollama reply is not a label", "ollama", `{"message":{"content":"It depends."}}`, "s", "not one of the labels", []string{"noul", "q", "--model", "m"}},
		{"ollama without logprobs", "ollama", `{"message":{"content":"yes"}}`, "s", "no log probabilities", []string{"noul", "q", "--model", "m"}},
		{"ollama too many choices", "ollama", "", "s", "at most 26", append([]string{"choice", "q", "--model", "m"}, strings.Fields(strings.Repeat("-o x ", 27))...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := call(t, tt.provider, tt.response, tt.stdin, tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want an error with %q", err, tt.want)
			}
		})
	}

	err := run([]string{"noul", "q", "--provider", "nope"}, strings.NewReader("s"), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Errorf("err = %v, want an unknown provider error", err)
	}

	for _, name := range []string{"openai", "ollama"} {
		_, _, _, err := call(t, name, "", "", "box", "q", "--model", "m", imageFile(t))
		if err == nil || !strings.Contains(err.Error(), "no box questions") {
			t.Errorf("%s: err = %v, want a box error", name, err)
		}
	}
}

func TestHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":"bad"}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	t.Setenv("OLLAMA_HOST", strings.TrimPrefix(srv.URL, "http://"))

	var out bytes.Buffer
	err := run([]string{"noul", "q", "--provider", "ollama", "--model", "m"}, strings.NewReader("s"), &out)
	if err == nil || !strings.Contains(err.Error(), "400") || !strings.Contains(out.String(), "bad") {
		t.Errorf("err = %v, output = %q", err, out.String())
	}
}
