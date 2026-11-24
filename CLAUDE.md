# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`jevry` is a small Go CLI. It sends input to a decision model. `--provider` selects the API: `typesafe` (default, Jev, `POST https://api.typesafe.ai/v1/systemone`), `openai` (Decisions API, `POST https://api.openai.com/v1/decisions`) or `ollama` (local model, `POST $OLLAMA_HOST/api/chat`). `main.go` has the CLI. Each provider is one `provider` value in its own file (`typesafe.go`, `openai.go`, `ollama.go`), registered in the `providers` map. It uses the standard library only. The tests in `main_test.go` run the CLI against a fake HTTP server.

## Commands

```sh
go build .            # builds ./jevry
go vet ./...
go test ./...
go run . file.json    # raw mode
echo "some state" | go run . --question "Is this spam?"
```

The program needs `TYPESAFE_API_KEY` (typesafe) or `OPENAI_API_KEY` (openai) in the environment. Ollama needs no key, but it needs `--model`.

## Behavior

- Input comes from the files in argv, or from stdin when argv is empty. `-` also means stdin. This copies Ruby's `ARGF.read` (`readARGF`). PNG and JPEG inputs are images (detected from the bytes). All other inputs are text, concatenated. Images need `--question`.
- Raw mode (no `--question`): the input must be a full request body for the provider, as a JSON object. The program prints the raw response.
- `--question` mode: the input is the `state`. The program wraps it in a request with one question with the key (TypeSafe) or name (OpenAI) `q`. The provider translates the question to its API shape. OpenAI calls yes/no `predicate`, and a `refusal` answer is an error. The question type depends on the other flags:
  - No other flags: yes/no. Prints the probability of yes with two decimals.
  - `--choice` (repeatable, `name` or `name=description`): `choice`. Prints the chosen option.
  - `--level` (repeatable, lowest first): `score`. Prints the score with two decimals.
  - `--box`: `box`. Prints `top=… bottom=… left=… right=…` with three decimals. It needs an image. Only TypeSafe has it.
  - Only one of `--choice`, `--level` and `--box` is permitted. All need `--question`.
- Images: TypeSafe gets them as base64 `assets` with random references. The state is the text (no images), the reference (one image, no text), or `{"text", "images"}`. OpenAI gets one user message with `input_text` and `input_image` parts. Ollama gets them in `images` of the user message.
- TypeSafe requests with `assets` or a box question get the header `TypeSafe-Danger-Zone-Images: true`, also in raw mode.
- Ollama has no decision API. The program gives each answer a one-token label (`yes`/`no`, `A`-`Z`, `0`-`9`), tells the model to reply with one label, and reads the probabilities from `top_logprobs` at the reply token (`ollamaProbabilities`). Raw mode sends the body to `/api/chat` with `stream: false` as default.
- Model: `--model` wins. Then the `model` of the request. Then the provider default (`jev-latest` or `gpt-6-luna`). Ollama has no default.
- On a non-200 response, the program prints the body to stdout and exits with status 1.
