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

Each question type is one command. `jevry --help` and `jevry <command> --help` show more examples.

| Command | Answer |
| --- | --- |
| `jevry noul <question>` | The probability of yes, from 0.00 to 1.00 |
| `jevry choice <question> -o <option>...` | The name of one option |
| `jevry score <question> -l <level>...` | A position on your scale, from 0 to the number of levels minus 1 |
| `jevry box <target> <image>` | The edges of a box around the target |
| `jevry raw` | The raw response to a full request body |

The input is the content that the question is about. `jevry` reads it from the files that you name after the question, or from stdin when you name no files. PNG and JPEG inputs are images. All other input is text.

```sh
echo "Win a free iPhone now!!!" | jevry noul "Is this spam?"
0.95

echo "My payouts are failing" | jevry choice "Which team should handle this?" \
  -o "billing=Payments, invoicing, refunds" -o technical -o sales -o other
billing

echo "Export fails in Safari but works in Chrome" | jevry score "How severe is this issue?" \
  -l "Appearance only" -l "A task fails, but another way works" -l "A task fails with no workaround"
1.01

jevry box "the coffee mug" kitchen.jpg
top=0.250 bottom=0.820 left=0.440 right=0.840
```

The edges of a box are fractions of the image size, measured from the top left corner. Only TypeSafe has box questions.

With TypeSafe, the state is the text when there are no images, and the image when the input is one image and no text. In all other cases the state is an object: the question can refer to `` `text` `` and `` `images[0]` ``.

### Providers

`--provider` (`-p`) selects the API. `--model` (`-m`) sets the model.

| Provider | Default model | Notes |
| --- | --- | --- |
| `typesafe` (default) | `jev-latest` | |
| `openai` | `gpt-6-luna` | No box questions |
| `ollama` | none: set `--model` | No box questions. At most 26 options and 10 levels |

```sh
echo "Win a free iPhone now!!!" | jevry noul "Is this spam?" -p openai
echo "Win a free iPhone now!!!" | jevry noul "Is this spam?" -p ollama -m gemma4:12b
```

Ollama has no decision API. `jevry` tells the model to reply with one label, and calculates the probabilities from the log probabilities of the labels. General models are not trained for calibrated probabilities: most answers are near 0 or 1.

### Raw requests

`jevry raw` sends a full request body for the provider, as a JSON object. It prints the raw JSON response. Use it for requests that the other commands cannot build, for example more than one question in one request.

```sh
jevry raw request.json
jevry raw -p openai request.json
jevry raw -p ollama -m gemma4:12b chat.json   # body for /api/chat
```

When the body has no `model` and you set no `--model`, `jevry` uses the default model of the provider.

## License

MIT
