# File Watcher Web App

Watches a file on host and publishes the content to web client

## Usage

```
$ go run main.go -h
Usage of main:
  -addr string
        http service address (default ":8080")
  -backend string
        backend url for frontend (default "localhost:8080")
  -dir string
        root dir to watch (default "watch-this-dir")
```

## Development

Run below and open http://localhost:8080

```
$ go run main.go
```

## Acknowledgments

* Build upon [gorilla/websocket example code](https://github.com/gorilla/websocket/tree/v1.4.2/examples/filewatch)
