# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`jevry` is a small Go CLI. It sends input to a decision model. `--provider` selects the API: `typesafe` (default, Jev, `POST https://api.typesafe.ai/v1/systemone`) or `openai` (Decisions API, `POST https://api.openai.com/v1/decisions`). All code is in `main.go`. Each provider is one entry in its `providers` map. It uses the standard library only. There are no tests yet.

## Commands

```sh
go build .            # builds ./jevry
go vet ./...
go run . file.json    # raw mode
echo "some state" | go run . --question "Is this spam?"
```

The program needs `TYPESAFE_API_KEY` (typesafe) or `OPENAI_API_KEY` (openai) in the environment.

## Behavior

- Input comes from the files in argv, concatenated, or from stdin when argv is empty. `-` also means stdin. This copies Ruby's `ARGF.read` (`readARGF`).
- Raw mode (no `--question`): the input must be a full request body for the provider, as a JSON object. The program prints the raw response.
- `--question` mode: the input is the `state`. The program wraps it in a request with one question with the key (TypeSafe) or name (OpenAI) `q`. The provider translates the question to its API shape. OpenAI calls yes/no `predicate`, and a `refusal` answer is an error. The question type depends on the other flags:
  - No other flags: yes/no. Prints the probability of yes with two decimals.
  - `--choice` (repeatable, `name` or `name=description`): `choice`. Prints the chosen option.
  - `--level` (repeatable, lowest first): `score`. Prints the score with two decimals.
  - `--choice` and `--level` together is an error. Both need `--question`.
- In both modes the program sets `model` to the provider default (`jev-latest` or `gpt-6-luna`) when the request has no `model`.
- On a non-200 response, the program prints the body to stdout and exits with status 1.
