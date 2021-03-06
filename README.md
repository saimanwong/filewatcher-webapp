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
```

## Development

Run below and open http://localhost:8080

```
$ go run main.go watch-this-dir
```

## Acknowledgments

* Build upon [gorilla/websocket example code](https://github.com/gorilla/websocket/tree/v1.4.2/examples/filewatch)

## Screenshots

[![](https://github.com/saimanwong/filewatcher-webapp/raw/master/screenshots/1-th.png)](https://github.com/saimanwong/filewatcher-webapp/raw/master/screenshots/1.png)
[![](https://github.com/saimanwong/filewatcher-webapp/raw/master/screenshots/2-th.png)](https://github.com/saimanwong/filewatcher-webapp/raw/master/screenshots/2.png)
