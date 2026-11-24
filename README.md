# jevry

`jevry` is a small command-line tool for decision models. It sends text to a model and prints the answer. It supports [TypeSafe](https://typesafe.ai)'s Jev model (the default), OpenAI's [Decisions API](https://developers.openai.com/api/docs/guides/decisions), and local models on [Ollama](https://ollama.com).

## Install

```sh
go install github.com/jeffrydegrande/jevry@latest
```

Set the API key for the provider that you use:

```sh
export TYPESAFE_API_KEY=...   # --provider typesafe (default)
export OPENAI_API_KEY=...     # --provider openai
```

`--provider ollama` needs no key. It uses the server at `OLLAMA_HOST` (default `localhost:11434`).

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

PNG and JPEG inputs are images. Name them as files, or send one on stdin. All question types accept images:

```sh
jevry --question "Is there a person at the door?" door.jpg
```

Add `--box` to find something in an image. `--question` names the target. `jevry` prints the edges of the box as fractions of the image size, measured from the top left corner. Only TypeSafe has box questions:

```sh
jevry --question "the coffee mug" --box kitchen.jpg
top=0.250 bottom=0.820 left=0.440 right=0.840
```

With TypeSafe, the state is the text when there are no images, and the image when the input is one image and no text. In all other cases the state is an object: the question can refer to `` `text` `` and `` `images[0]` ``.

### Providers

Add `--provider openai` to send the question to OpenAI instead of TypeSafe. The output is the same:

```sh
echo "Win a free iPhone now!!!" | jevry --provider openai --question "Is this spam?"
```

Add `--provider ollama` and `--model` to use a local model:

```sh
echo "Win a free iPhone now!!!" | jevry --provider ollama --model gemma4:12b --question "Is this spam?"
```

Ollama has no decision API. `jevry` tells the model to reply with one label, and calculates the probabilities from the log probabilities of the labels. General models are not trained for calibrated probabilities: most answers are near 0 or 1. The limits are 26 choices and 10 levels.

### Raw requests

Send a full request body as JSON for the provider. `jevry` prints the raw JSON response:

```sh
jevry request.json
jevry --provider openai request.json
jevry --provider ollama --model gemma4:12b chat.json   # body for /api/chat
```

`--model` sets the model. Without it, `jevry` keeps the `model` of the request, or uses `jev-latest` for TypeSafe and `gpt-6-luna` for OpenAI. Ollama has no default model.

## License

MIT
