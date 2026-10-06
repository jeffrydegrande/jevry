# jevry

`jevry` is a small command-line tool for [TypeSafe](https://typesafe.ai)'s Jev model. It sends text to Jev and prints the answer.

## Install

```sh
go install github.com/jeffrydegrande/jevry@latest
```

Set your API key:

```sh
export TYPESAFE_API_KEY=...
```

## Usage

`jevry` reads input from the files that you name, or from stdin when you name no files.

Ask a yes/no question about the input. `jevry` prints the probability of "yes":

```sh
echo "Win a free iPhone now!!!" | jevry --question "Is this spam?"
0.94
```

Send a full TypeSafe request body as JSON. `jevry` prints the raw JSON response:

```sh
jevry request.json
```

When the request has no `model`, `jevry` uses `jev-latest`.

## License

MIT
