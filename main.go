// Copyright 2013 The Gorilla WebSocket Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write the file to the client.
	writeWait = 1 * time.Second

	// Time allowed to read the next pong message from the client.
	pongWait = 60 * time.Second

	// Send pings to client with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Poll file for changes with this period.
	filePeriod = 1 * time.Second
)

var (
	addr       = flag.String("addr", ":8080", "http service address")
	backendURL = flag.String("backend", "localhost:8080", "backend url for frontend")
	rootDir    = flag.String("dir", "watch-this-dir", "root dir to watch")
	homeTempl  = template.Must(template.New("index.html").Delims("[[", "]]").ParseFiles("static/index.html"))
	upgrader   = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
)

type File struct {
	IsDir    bool   `json:"isDir"`
	Name     string `json:"name"`
	FullPath string `json:"fullPath"`
	Depth    int    `json:"depth"`
	Show     bool   `json:"show"`
	Expanded bool   `json:"expanded"`
}

func readFileIfModified(lastMod time.Time, filename string) ([]byte, time.Time, error) {
	fi, err := os.Stat(filename)
	if err != nil {
		return nil, lastMod, err
	}
	if !fi.ModTime().After(lastMod) {
		return nil, lastMod, nil
	}
	p, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fi.ModTime(), err
	}
	return p, fi.ModTime(), nil
}

func reader(ws *websocket.Conn) {
	defer ws.Close()
	ws.SetReadLimit(512)
	ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error { ws.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			break
		}
	}
}

func writer(ws *websocket.Conn, lastMod time.Time, filename string) {
	lastError := ""
	pingTicker := time.NewTicker(pingPeriod)
	fileTicker := time.NewTicker(filePeriod)
	defer func() {
		pingTicker.Stop()
		fileTicker.Stop()
		ws.Close()
	}()
	for {
		select {
		case <-fileTicker.C:
			var p []byte
			var err error

			p, lastMod, err = readFileIfModified(lastMod, filename)

			if err != nil {
				if s := err.Error(); s != lastError {
					lastError = s
					p = []byte(lastError)
				}
			} else {
				lastError = ""
			}

			if p != nil {
				ws.SetWriteDeadline(time.Now().Add(writeWait))
				if err := ws.WriteMessage(websocket.TextMessage, p); err != nil {
					return
				}
			}
		case <-pingTicker.C:
			ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := ws.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		}
	}
}

func serveWs(w http.ResponseWriter, r *http.Request) {
	filename := r.FormValue("filename")
	log.Printf("%s requested for %s", r.Host, filename)

	// Validate request to prevent path traversal
	// https://owasp.org/www-community/attacks/Path_Traversal
	fs := strings.Split(filename, "/")
	if fs[0] != *rootDir {
		log.Printf("%s requested for non rootDir path %s", r.Host, filename)
		return
	}

	rc, _ := regexp.Compile(`^[a-zA-Z0-9\/-]*\.?[a-zA-Z0-9\/-]*$`)
	if !rc.MatchString(filename) {
		log.Printf("%s requested for path %s which does not match regex", r.Host, filename)
		return
	}

	// Setup WebSocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		if _, ok := err.(websocket.HandshakeError); !ok {
			log.Println(err)
		}
		return
	}
	var lastMod time.Time
	if n, err := strconv.ParseInt(r.FormValue("lastMod"), 16, 64); err == nil {
		lastMod = time.Unix(0, n)
	}

	go writer(ws, lastMod, filename)
	reader(ws)
}

func getPaths() []File {
	paths := []File{}
	err := filepath.Walk(*rootDir,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			files := strings.Split(path, "/")
			depth := len(files)

			paths = append(paths, File{
				IsDir:    info.IsDir(),
				Name:     files[len(files)-1],
				FullPath: path,
				Depth:    depth,
				Show:     true,
				Expanded: true,
			})
			return nil
		},
	)

	if err != nil {
		log.Println(err)
	}

	sort.SliceStable(paths, func(i, j int) bool {
		return paths[i].FullPath < paths[j].FullPath
	})

	return paths
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	paths := getPaths()

	if r.URL.Path != "/" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	lastMod := time.Unix(0, 0)
	var v = struct {
		BackendURL string
		LastMod    string
		Paths      []File
	}{
		*backendURL,
		strconv.FormatInt(lastMod.UnixNano(), 16),
		paths,
	}
	log.Printf("%s connected", r.Host)
	homeTempl.Execute(w, &v)
}

func main() {
	flag.Parse()
	http.HandleFunc("/", serveHome)
	http.HandleFunc("/ws", serveWs)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatal(err)
	}
}