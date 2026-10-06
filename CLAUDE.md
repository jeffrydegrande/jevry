# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`jevry` is a small Go CLI. It sends input to the TypeSafe Jev model (`POST https://api.typesafe.ai/v1/systemone`). All code is in `main.go`. It uses the standard library only. There are no tests yet.

## Commands

```sh
go build .            # builds ./jevry
go vet ./...
go run . file.json    # raw mode
echo "some state" | go run . --question "Is this spam?"
```

The program needs `TYPESAFE_API_KEY` in the environment.

## Behavior

- Input comes from the files in argv, concatenated, or from stdin when argv is empty. `-` also means stdin. This copies Ruby's `ARGF.read` (`readARGF`).
- Raw mode (no `--question`): the input must be a full TypeSafe request body as a JSON object. The program prints the raw response.
- `--question` mode: the input is the `state`. The program wraps it in a request with one `noul` (yes/no) question with the key `q`. It prints `answers.q.noul` as a probability with two decimals.
- In both modes the program sets `model` to `jev-latest` when the request has no `model`.
- On a non-200 response, the program prints the body to stdout and exits with status 1.
