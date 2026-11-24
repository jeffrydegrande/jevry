# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`jevry` is a small Go CLI. It sends input to a decision model. `--provider` selects the API: `typesafe` (default, Jev, `POST https://api.typesafe.ai/v1/systemone`), `openai` (Decisions API, `POST https://api.openai.com/v1/decisions`) or `ollama` (local model, `POST $OLLAMA_HOST/api/chat`). `main.go` has the CLI. It uses [cobra](https://github.com/spf13/cobra): each question type is one subcommand (`noul`, `choice`, `score`, `box`), plus `raw`. All subcommands call `app.ask`. Each provider is one `provider` value in its own file (`typesafe.go`, `openai.go`, `ollama.go`), registered in the `providers` map. cobra is the only dependency. The tests in `main_test.go` run the CLI against a fake HTTP server.

## Commands

```sh
go build .            # builds ./jevry
go vet ./...
go test ./...
go run . raw file.json
echo "some state" | go run . noul "Is this spam?"
```

The program needs `TYPESAFE_API_KEY` (typesafe) or `OPENAI_API_KEY` (openai) in the environment. Ollama needs no key, but it needs `--model`.

## Behavior

- Input comes from the files in argv, or from stdin when argv is empty. `-` also means stdin. This copies Ruby's `ARGF.read` (`readARGF`). PNG and JPEG inputs are images (detected from the bytes). All other inputs are text, concatenated. `raw` does not accept images.
- `raw`: the input must be a full request body for the provider, as a JSON object. The program prints the raw response.
- Question commands: the first argument is the question. The other arguments are the input files. The input is the `state`. The program wraps it in a request with one question with the key (TypeSafe) or name (OpenAI) `q`. The provider translates the question to its API shape. OpenAI calls yes/no `predicate`, and a `refusal` answer is an error.
  - `noul`: yes/no. Prints the probability of yes with two decimals.
  - `choice` with `--option`/`-o` (repeatable, `name` or `name=description`): prints the chosen option.
  - `score` with `--level`/`-l` (repeatable, lowest first): prints the score with two decimals.
  - `box`: prints `top=… bottom=… left=… right=…` with three decimals. It needs an image. Only TypeSafe has it.
- Each subcommand has `Long` text and an `Example` block that show what the question type can do. Keep the example outputs equal to real outputs.
- Images: TypeSafe gets them as base64 `assets` with random references. The state is the text (no images), the reference (one image, no text), or `{"text", "images"}`. OpenAI gets one user message with `input_text` and `input_image` parts. Ollama gets them in `images` of the user message.
- TypeSafe requests with `assets` or a box question get the header `TypeSafe-Danger-Zone-Images: true`, also in raw mode.
- Ollama has no decision API. The program gives each answer a one-token label (`yes`/`no`, `A`-`Z`, `0`-`9`), tells the model to reply with one label, and reads the probabilities from `top_logprobs` at the reply token (`ollamaProbabilities`). `raw` sends the body to `/api/chat` with `stream: false` as default.
- Model: `--model` wins. Then the `model` of the request. Then the provider default (`jev-latest` or `gpt-6-luna`). Ollama has no default.
- On a non-200 response, the program prints the body to stdout and exits with status 1.
