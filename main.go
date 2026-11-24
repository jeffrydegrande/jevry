// jevry reads input from the files named in argv, or from stdin when argv is
// empty (like Ruby's ARGF.read), and sends it to a decision model. The
// --provider flag selects the API: typesafe (Jev, the default), openai (the
// Decisions API) or ollama (a local model).
//
// Without --question, the input is a full request body for the provider. The
// program prints the raw response.
//
// With --question, the input is the state. PNG and JPEG inputs are images. All
// other input is text. The question type depends on the other flags:
//   - no other flags: yes/no. Prints the probability of yes.
//   - --choice (repeatable): choice. Prints the chosen option.
//   - --level (repeatable): score. Prints the score, from 0 to len(levels)-1.
//   - --box: box. Prints the edges of the box around the target in the image.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
)

// input is the content that the questions refer to.
type input struct {
	text   string
	images []image
}

// image is one PNG or JPEG file. Format is "png" or "jpg".
type image struct {
	data   []byte
	format string
}

// question is one question in a provider-neutral form. With no choices, no
// levels and no box, it is a yes/no question.
type question struct {
	text    string
	choices []option
	levels  []string
	box     bool
}

type option struct{ name, desc string }

// answer is the parsed answer to a question. Type is "noul", "choice", "score"
// or "box". Noul is the probability of yes.
type answer struct {
	Type   string
	Noul   float64
	Choice any
	Score  float64
	Box    box
}

// box holds the edges of a box as fractions of the image size, measured from
// the top left corner.
type box struct{ Top, Bottom, Left, Right float64 }

// provider describes one API.
type provider struct {
	// keyEnv is the environment variable with the API key. Empty means that
	// the API needs no key.
	keyEnv string
	// model is the default model. Empty means that --model is necessary.
	model string
	url   func() string
	// request builds the request body for one question.
	request func(in input, q question) (map[string]any, error)
	// answer parses the response to a request that request built.
	answer func(body []byte, q question) (answer, error)
	// finish completes each request, also in raw mode. It can add defaults to
	// the body and headers to the HTTP request. It can be nil.
	finish func(req map[string]any, h http.Header)
}

var providers = map[string]provider{
	"typesafe": typesafe,
	"openai":   openai,
	"ollama":   ollama,
}

// errUsage reports that the flag package already printed the error.
var errUsage = errors.New("usage")

func main() {
	err := run(os.Args[1:], os.Stdin, os.Stdout)
	if errors.Is(err, errUsage) {
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "jevry:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("jevry", flag.ContinueOnError)
	providerName := fs.String("provider", "typesafe", "API to use: "+providerNames())
	model := fs.String("model", "", "model to use (default: the provider default)")
	var q question
	fs.StringVar(&q.text, "question", "", "question to ask about the input (yes/no unless --choice, --level or --box is set)")
	fs.Func("choice", "option for a choice question, as `name` or name=description (repeatable)", func(s string) error {
		name, desc, _ := strings.Cut(s, "=")
		q.choices = append(q.choices, option{name, desc})
		return nil
	})
	fs.Func("level", "level description for a score question, lowest first (repeatable)", func(s string) error {
		q.levels = append(q.levels, s)
		return nil
	})
	fs.BoolVar(&q.box, "box", false, "ask a box question: --question names the target to find in the image")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errUsage
	}

	p, ok := providers[*providerName]
	if !ok {
		return fmt.Errorf("unknown provider %q (use %s)", *providerName, providerNames())
	}
	types := 0
	for _, set := range []bool{q.choices != nil, q.levels != nil, q.box} {
		if set {
			types++
		}
	}
	switch {
	case types > 1:
		return fmt.Errorf("use only one of --choice, --level and --box")
	case types == 1 && q.text == "":
		return fmt.Errorf("--choice, --level and --box need --question")
	}

	in, err := readARGF(fs.Args(), stdin)
	if err != nil {
		return err
	}

	var req map[string]any
	switch {
	case q.text != "":
		if q.box && len(in.images) == 0 {
			return fmt.Errorf("--box needs a PNG or JPEG image in the input")
		}
		if req, err = p.request(in, q); err != nil {
			return err
		}
	case len(in.images) > 0:
		return fmt.Errorf("an image in the input needs --question")
	default:
		if err := json.Unmarshal([]byte(in.text), &req); err != nil {
			return fmt.Errorf("input is not a JSON object: %w", err)
		}
	}
	if *model != "" {
		req["model"] = *model
	} else if _, ok := req["model"]; !ok {
		if p.model == "" {
			return fmt.Errorf("provider %s has no default model: set --model", *providerName)
		}
		req["model"] = p.model
	}

	httpReq, _ := http.NewRequest("POST", p.url(), nil)
	httpReq.Header.Set("Content-Type", "application/json")
	if p.keyEnv != "" {
		key := os.Getenv(p.keyEnv)
		if key == "" {
			return fmt.Errorf("%s is not set", p.keyEnv)
		}
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}
	if p.finish != nil {
		p.finish(req, httpReq.Header)
	}
	body, _ := json.Marshal(req)
	httpReq.Body = io.NopCloser(bytes.NewReader(body))
	httpReq.ContentLength = int64(len(body))

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
		fmt.Fprintf(stdout, "%s\n", out)
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	if q.text == "" {
		fmt.Fprintf(stdout, "%s\n", out)
		return nil
	}

	a, err := p.answer(out, q)
	if err != nil {
		return err
	}
	switch a.Type {
	case "noul":
		fmt.Fprintf(stdout, "%.2f\n", a.Noul)
	case "choice":
		fmt.Fprintln(stdout, a.Choice)
	case "score":
		fmt.Fprintf(stdout, "%.2f\n", a.Score)
	case "box":
		fmt.Fprintf(stdout, "top=%.3f bottom=%.3f left=%.3f right=%.3f\n", a.Box.Top, a.Box.Bottom, a.Box.Left, a.Box.Right)
	default:
		return fmt.Errorf("unexpected answer type %q", a.Type)
	}
	return nil
}

func providerNames() string {
	var names []string
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// readARGF reads the named files, or stdin when there are none. A name of "-"
// also means stdin. PNG and JPEG inputs become images. All other inputs are
// text, concatenated in the given order.
func readARGF(names []string, stdin io.Reader) (input, error) {
	if len(names) == 0 {
		names = []string{"-"}
	}
	var in input
	var text bytes.Buffer
	for _, name := range names {
		var b []byte
		var err error
		if name == "-" {
			b, err = io.ReadAll(stdin)
		} else {
			b, err = os.ReadFile(name)
		}
		if err != nil {
			return input{}, err
		}
		switch http.DetectContentType(b) {
		case "image/png":
			in.images = append(in.images, image{b, "png"})
		case "image/jpeg":
			in.images = append(in.images, image{b, "jpg"})
		default:
			text.Write(b)
		}
	}
	in.text = text.String()
	return in, nil
}
