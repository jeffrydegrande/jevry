// jevry reads input from the files named in argv, or from stdin when argv is
// empty (like Ruby's ARGF.read), and sends it to Jev.
//
// Without --question, the input is a full TypeSafe request body. The program
// prints the raw response.
//
// With --question, the input is the state. The program asks the question as a
// yes/no (noul) question and prints the probability of yes.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "jevry:", err)
		os.Exit(1)
	}
}

func run() error {
	question := flag.String("question", "", "yes/no question to ask about the input")
	flag.Parse()

	in, err := readARGF(flag.Args())
	if err != nil {
		return err
	}

	var req map[string]any
	if *question != "" {
		req = map[string]any{
			"state": string(in),
			"questions": map[string]any{
				"q": map[string]any{"type": "noul", "instructions": *question},
			},
		}
	} else if err := json.Unmarshal(in, &req); err != nil {
		return fmt.Errorf("input is not a JSON object: %w", err)
	}
	if _, ok := req["model"]; !ok {
		req["model"] = "jev-latest"
	}
	body, _ := json.Marshal(req)

	key := os.Getenv("TYPESAFE_API_KEY")
	if key == "" {
		return fmt.Errorf("TYPESAFE_API_KEY is not set")
	}

	httpReq, _ := http.NewRequest("POST", "https://api.typesafe.ai/v1/systemone", bytes.NewReader(body))
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
	if *question == "" {
		os.Stdout.Write(out)
		fmt.Println()
		return nil
	}

	var res struct {
		Answers struct {
			Q struct {
				Noul float64 `json:"noul"`
			} `json:"q"`
		} `json:"answers"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return err
	}
	fmt.Printf("%.2f\n", res.Answers.Q.Noul)
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
