# jevry

`jevry` is a small command-line tool for decision models. It sends text to a model and prints the answer. It supports [TypeSafe](https://typesafe.ai)'s Jev model (the default) and OpenAI's [Decisions API](https://developers.openai.com/api/docs/guides/decisions).

## Install

```sh
go install github.com/jeffrydegrande/jevry@latest
```

Set the API key for the provider that you use:

```sh
export TYPESAFE_API_KEY=...   # --provider typesafe (default)
export OPENAI_API_KEY=...     # --provider openai
```

## Usage

`jevry` reads input from the files that you name, or from stdin when you name no files.

Ask a yes/no question about the input. `jevry` prints the probability of "yes":

```sh
echo "Win a free iPhone now!!!" | jevry --question "Is this spam?"
0.94
```

Add `--choice` to pick one option. Repeat it for each option. Use `name=description` to describe an option. `jevry` prints the chosen option:

```sh
echo "My payouts are failing" | jevry --question "Which team should handle this?" \
  --choice "billing=Payments, invoicing, refunds" --choice technical --choice sales
billing
```

Add `--level` to rate the input on a scale. Repeat it for each level, lowest first. `jevry` prints the score, from 0 to the number of levels minus 1:

```sh
echo "My payouts are failing" | jevry --question "How frustrated is the customer?" \
  --level Calm --level Frustrated --level "Very angry"
1.04
```

Add `--provider openai` to send the question to OpenAI instead of TypeSafe. The output is the same:

```sh
echo "Win a free iPhone now!!!" | jevry --provider openai --question "Is this spam?"
```

Send a full request body as JSON for the provider. `jevry` prints the raw JSON response:

```sh
jevry request.json
jevry --provider openai request.json
```

When the request has no `model`, `jevry` uses `jev-latest` for TypeSafe and `gpt-6-luna` for OpenAI.

## License

MIT
