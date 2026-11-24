// jevry sends input to a decision model and prints the answer. Each question
// type is one subcommand: noul, choice, score and box. The raw subcommand
// sends a full request body. The --provider flag selects the API: typesafe
// (Jev, the default), openai (the Decisions API) or ollama (a local model).
//
// Input comes from the files named after the question, or from stdin when
// there are none (like Ruby's ARGF.read). PNG and JPEG inputs are images. All
// other input is text.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
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

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "jevry:", err)
		os.Exit(1)
	}
}

// app holds the flags that all subcommands share.
type app struct {
	provider, model string
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	var a app
	root := &cobra.Command{
		Use:   "jevry",
		Short: "Ask a decision model a typed question about text and images",
		Long: `jevry sends input to a decision model and prints the answer.

A decision model does not write text. It gives a typed answer that a script
can use: a probability, one of your options, a score, or a box in an image.
Each question type is one command.

The input is the content that the question is about. It comes from the files
that you name after the question, or from stdin when you name no files. "-"
also means stdin. PNG and JPEG inputs are images. All other input is text.

Providers:
  typesafe  Jev, from TypeSafe. The default. Needs TYPESAFE_API_KEY.
  openai    The OpenAI Decisions API. Needs OPENAI_API_KEY.
  ollama    A local model. Needs --model. Uses OLLAMA_HOST (default
            localhost:11434). Local models are not trained for calibrated
            probabilities: most answers are near 0 or 1.`,
		Example: `  # Is a condition true? Prints the probability of yes.
  echo "Win a free iPhone now!!!" | jevry noul "Is this spam?"

  # Which option fits? Prints the name of the option.
  jevry choice "Which team should handle this?" -o billing -o technical -o sales ticket.txt

  # How much? Prints a position on your scale.
  jevry score "How urgent is this?" -l "Can wait" -l "This week" -l "Today" ticket.txt

  # Where is it? Prints the edges of a box in the image.
  jevry box "the coffee mug" kitchen.jpg`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVarP(&a.provider, "provider", "p", "typesafe", "`API` to use: "+providerNames())
	root.PersistentFlags().StringVarP(&a.model, "model", "m", "", "`model` to use (default: jev-latest for typesafe, gpt-6-luna for openai)")
	root.AddCommand(a.noulCmd(), a.choiceCmd(), a.scoreCmd(), a.boxCmd(), a.rawCmd())
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	return root.Execute()
}

func (a *app) noulCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "noul <question> [file...]",
		Short: "Ask a yes/no question and get the probability of yes",
		Long: `Ask a yes/no question about the input. A statement is also permitted.

The answer is the probability of yes, from 0.00 to 1.00. A value near 0.50
means that yes and no are equally probable. It does not mean "medium".
Compare the value with a threshold that fits the cost of an error.

Use noul to detect, filter and verify. When more than one label can apply,
ask one noul for each label.`,
		Example: `  # Detect
  echo "Win a free iPhone now!!!" | jevry noul "Is this spam?"
  0.95

  # Verify a claim against a source
  jevry noul "Does the contract let a party end the agreement early?" contract.txt

  # Use the answer in a script
  jevry noul "Does this alert need a person now?" alert.txt | awk '{exit !($1 > 0.8)}' && page-oncall

  # Ask about an image
  jevry noul "Is there a person at the door?" door.jpg

  # Compare a text with an image (TypeSafe names the parts text and images[0])
  jevry noul 'Does the description in ` + "`text`" + ` match the product in ` + "`images[0]`" + `?' listing.txt product.jpg`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ask(cmd, question{text: args[0]}, args[1:])
		},
	}
}

func (a *app) choiceCmd() *cobra.Command {
	var options []string
	cmd := &cobra.Command{
		Use:   "choice <question> --option <name>[=<description>]... [file...]",
		Short: "Pick one option from a list that you define",
		Long: `Ask the model to pick one option. Give each option with --option.

The answer is the name of the option with the highest probability. A
description tells the model when the option applies. The name is what your
script receives.

Use choice to classify and to route. Add an option such as "other" when
it is possible that no option fits: the model always picks one.`,
		Example: `  # Route
  echo "My payouts are failing" | jevry choice "Which team should handle this?" \
    -o "billing=Payments, invoicing, refunds" -o technical -o sales -o other
  billing

  # Classify
  jevry choice "What is the tone of this message?" -o angry -o calm -o excited mail.txt

  # Select the next step
  jevry choice "What must the agent do next?" \
    -o "retry=The failure is temporary" \
    -o "escalate=A person must decide" \
    -o "close=The problem is solved" incident.txt

  # Classify an image
  jevry choice "What is the person carrying?" -o package -o bag -o nothing door.jpg`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := question{text: args[0]}
			for _, o := range options {
				name, desc, _ := strings.Cut(o, "=")
				q.choices = append(q.choices, option{name, desc})
			}
			return a.ask(cmd, q, args[1:])
		},
	}
	cmd.Flags().StringArrayVarP(&options, "option", "o", nil, "one `option`, as name or name=description (repeat for each option)")
	cmd.MarkFlagRequired("option")
	return cmd
}

func (a *app) scoreCmd() *cobra.Command {
	var levels []string
	cmd := &cobra.Command{
		Use:   "score <question> --level <description>... [file...]",
		Short: "Rate the input on a scale of levels that you define",
		Long: `Ask the model to rate the input. Give each level with --level, lowest first.

The answer is a number from 0 to the number of levels minus 1. It is the
average of the levels, weighted by their probabilities. Thus it can be
between two levels: 1.70 on a scale of three levels is near the top level.

Use score to rank and to set a priority. Write each level as a concrete
situation that is clear on its own. Use 2 to 10 levels.`,
		Example: `  # Set a priority
  echo "Export fails in Safari but works in Chrome" | jevry score "How severe is this issue?" \
    -l "Appearance only" -l "A task fails, but another way works" -l "A task fails with no workaround"
  1.01

  # Rank: score each file, then sort
  for f in tickets/*.txt; do echo "$(jevry score 'How urgent is this?' \
    -l 'Can wait' -l 'This week' -l 'Today' "$f") $f"; done | sort -rn

  # Measure a quality
  jevry score "How complete is this bug report?" \
    -l "No steps to reproduce" -l "Steps, but no expected result" -l "Steps and expected result" report.txt`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ask(cmd, question{text: args[0], levels: levels}, args[1:])
		},
	}
	cmd.Flags().StringArrayVarP(&levels, "level", "l", nil, "`description` of one level, lowest first (repeat for each level)")
	cmd.MarkFlagRequired("level")
	return cmd
}

func (a *app) boxCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "box <target> <image>...",
		Short: "Find where something is in an image (typesafe only)",
		Long: `Find the target in the image. The target is a description, not a question.

The answer is the smallest box that contains the target. Each edge is a
fraction of the image size, measured from the top left corner. Multiply
left and right by the width, and top and bottom by the height, to get pixels.

The answer is always a box, also when the target is not in the image. Ask a
noul first when it is possible that the target is absent. One box question
finds one target: describe the target so that only one thing fits.

Only the typesafe provider has box questions.`,
		Example: `  # Find an object
  jevry box "the coffee mug" kitchen.jpg
  top=0.250 bottom=0.820 left=0.440 right=0.840

  # Find a field on a scanned form
  jevry box "the signature" contract-page-4.png

  # Describe the target so that only one thing fits
  jevry box "the mug nearest the sink" kitchen.jpg

  # Make sure that the target is there
  jevry noul "Is there a coffee mug in the image?" kitchen.jpg`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ask(cmd, question{text: args[0], box: true}, args[1:])
		},
	}
}

func (a *app) rawCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "raw [file...]",
		Short: "Send a full request body and print the raw response",
		Long: `Send a full request body for the provider, as a JSON object. The program
prints the raw JSON response. It adds the model when the body has none.

Use raw for requests that the other commands cannot build: more than one
question in one request, structured state, or criteria for a noul.

The body is for this endpoint:
  typesafe  POST /v1/systemone  https://docs.typesafe.ai/api/system-one
  openai    POST /v1/decisions  https://developers.openai.com/api/docs/guides/decisions
  ollama    POST /api/chat      https://docs.ollama.com/api/chat`,
		Example: `  # Ask two questions about the same state in one request
  jevry raw <<'EOF'
  {
    "state": {"subject": "Duplicate charge", "message": "Please refund me."},
    "questions": {
      "billing": {"type": "noul", "instructions": "Is this message about billing?"},
      "tone": {"type": "choice", "instructions": "What is the tone?",
               "criteria": {"angry": null, "calm": null}}
    }
  }
  EOF

  jevry raw --provider openai request.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ask(cmd, question{}, args)
		},
	}
}

// ask sends one request and prints the answer. An empty question means raw
// mode: the input is the request body, and the output is the raw response.
func (a *app) ask(cmd *cobra.Command, q question, files []string) error {
	stdout := cmd.OutOrStdout()
	p, ok := providers[a.provider]
	if !ok {
		return fmt.Errorf("unknown provider %q (use %s)", a.provider, providerNames())
	}
	in, err := readARGF(files, cmd.InOrStdin())
	if err != nil {
		return err
	}

	var req map[string]any
	switch {
	case q.text != "":
		if q.box && len(in.images) == 0 {
			return fmt.Errorf("box needs a PNG or JPEG image in the input")
		}
		if req, err = p.request(in, q); err != nil {
			return err
		}
	case len(in.images) > 0:
		return fmt.Errorf("the input of raw must be JSON, not an image")
	default:
		if err := json.Unmarshal([]byte(in.text), &req); err != nil {
			return fmt.Errorf("input is not a JSON object: %w", err)
		}
	}
	if a.model != "" {
		req["model"] = a.model
	} else if _, ok := req["model"]; !ok {
		if p.model == "" {
			return fmt.Errorf("provider %s has no default model: set --model", a.provider)
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

	ans, err := p.answer(out, q)
	if err != nil {
		return err
	}
	switch ans.Type {
	case "noul":
		fmt.Fprintf(stdout, "%.2f\n", ans.Noul)
	case "choice":
		fmt.Fprintln(stdout, ans.Choice)
	case "score":
		fmt.Fprintf(stdout, "%.2f\n", ans.Score)
	case "box":
		fmt.Fprintf(stdout, "top=%.3f bottom=%.3f left=%.3f right=%.3f\n", ans.Box.Top, ans.Box.Bottom, ans.Box.Left, ans.Box.Right)
	default:
		return fmt.Errorf("unexpected answer type %q", ans.Type)
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
