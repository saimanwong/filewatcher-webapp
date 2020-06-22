// Copyright 2013 The Gorilla WebSocket Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"errors"
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
	homeTempl  = template.Must(template.New("index.html").Delims("[[", "]]").ParseFiles("static/index.html"))
	upgrader   = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	fullPath  string
	basePath  string
	watchPath string
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
	log.Printf("INFO %s requested for %s", r.RemoteAddr, filename)

	// Validate filename
	err := validateFilename(filename)
	if err != nil {
		log.Printf("WARN %s requested for %s, but failed to validate (%s)", r.RemoteAddr, filename, err)
		return
	}

	// Setup WebSocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		if _, ok := err.(websocket.HandshakeError); !ok {
			log.Printf("WARN %s", err)
		}
		return
	}
	var lastMod time.Time
	if n, err := strconv.ParseInt(r.FormValue("lastMod"), 16, 64); err == nil {
		lastMod = time.Unix(0, n)
	}

	fullFilename := basePath + filename
	go writer(ws, lastMod, fullFilename)
	reader(ws)
}

// Validate request to prevent path traversal
// https://owasp.org/www-community/attacks/Path_Traversal
// Returns nil if filename is ok, otherwise error
func validateFilename(filename string) error {
	fs := strings.Split(filename, "/")
	if fs[0] != watchPath {
		return errors.New("filename must begin with the path")
	}

	rc, _ := regexp.Compile(`^[a-zA-Z0-9\/-]*\.?[a-zA-Z0-9\/-]*$`)
	if !rc.MatchString(filename) {
		return errors.New("filename must only contain one dot")
	}

	return nil
}

func getPaths() []File {
	paths := []File{}
	err := filepath.Walk(fullPath,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			modifiedPath := strings.Replace(path, basePath, "", -1)
			files := strings.Split(modifiedPath, "/")
			depth := len(files) - 1

			paths = append(paths, File{
				IsDir:    info.IsDir(),
				Name:     files[len(files)-1],
				FullPath: modifiedPath,
				Depth:    depth,
				Show:     true,
				Expanded: true,
			})
			return nil
		},
	)

	if err != nil {
		log.Printf("WARN %s", err)
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
	log.Printf("INFO %s connected", r.RemoteAddr)
	homeTempl.Execute(w, &v)
}

func main() {
	flag.Parse()
	if flag.NArg() != 1 {
		log.Fatal("ERR no file or directory specified")
	}

	fullPath = flag.Args()[0]

	_, err := os.Stat(fullPath)
	if err != nil {
		log.Fatalf("ERR %s", err)
	}

	pathSpl := strings.Split(fullPath, "/")
	watchPath = pathSpl[len(pathSpl)-1]
	basePath = ""
	if len(pathSpl) > 1 {
		basePath = strings.Join(pathSpl[:len(pathSpl)-1], "/") + "/"
	}

	http.HandleFunc("/", serveHome)
	http.HandleFunc("/ws", serveWs)

	log.Printf("INFO backend listens on %s", *addr)
	log.Printf("INFO frontend connects to %s", *backendURL)
	log.Printf("INFO going to watch %s", fullPath)

	err = http.ListenAndServe(*addr, nil)
	if err != nil {
		log.Fatalf("ERR %s", err)
	}
}
