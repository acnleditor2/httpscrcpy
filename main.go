package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type portState struct {
	listener                  net.Listener
	videoSocket               net.Conn
	audioSocket               net.Conn
	controlSocket             net.Conn
	connectionControlChannel  chan bool
	videoConnectedChannel     chan struct{}
	audioConnectedChannel     chan struct{}
	clipboardChannel          chan string
	uhidKeyboardOutputChannel chan string
	sendVideoSocket           net.Conn
	sendAudioSocket           net.Conn
	deviceName                string
	videoCodec                uint32
	audioCodec                uint32
	initialVideoWidth         uint32
	initialVideoHeight        uint32
	scrcpyServer              *exec.Cmd
}

type Port struct {
	Video                       bool     `json:"video"`
	Audio                       bool     `json:"audio"`
	Control                     bool     `json:"control"`
	Forward                     bool     `json:"forward"`
	UhidKeyboardReportDesc      string   `json:"uhidKeyboardReportDesc"`
	UhidMouseReportDesc         string   `json:"uhidMouseReportDesc"`
	VideoExtension              string   `json:"videoExtension"`
	AudioExtension              string   `json:"audioExtension"`
	ClipboardStreamExtension    string   `json:"clipboardStreamExtension"`
	UhidKeyboardOutputExtension string   `json:"uhidKeyboardOutputExtension"`
	ADB                         []string `json:"adb"`
	ScrcpyServer                []string `json:"scrcpyServer"`
	ScrcpyServerOptions         []string `json:"scrcpyServerOptions"`
	ClipboardAutosync           bool     `json:"clipboardAutosync"`
	Cleanup                     bool     `json:"cleanup"`
	PowerOn                     bool     `json:"powerOn"`
}

type Config struct {
	Address    string              `json:"address"`
	Static     string              `json:"static"`
	Cert       string              `json:"cert"`
	Key        string              `json:"key"`
	Ports      map[int]Port        `json:"ports"`
	Users      map[string]User     `json:"users"`
	Endpoints  map[string][]string `json:"endpoints"`
	Extensions [][]string          `json:"extensions"`
}

var (
	portMap     = map[int]*portState{}
	endpointMap = map[string]map[string]struct{}{}
)

var config Config

func getPort(portString string) int {
	if portString == "" && len(config.Ports) == 1 {
		for port := range config.Ports {
			return port
		}
	}

	port, err := strconv.Atoi(portString)
	if err != nil {
		return 0
	}

	return port
}

func readDummyByte(c net.Conn) bool {
	data := make([]byte, 1)

	n, err := c.Read(data)
	if err != nil {
		return false
	}
	if n != 1 {
		return false
	}

	return true
}

func readDeviceMeta(port int) bool {
	ps := portMap[port]
	data := make([]byte, 64)
	var n int
	var err error

	if config.Ports[port].Video {
		n, err = io.ReadFull(ps.videoSocket, data)
	} else if config.Ports[port].Audio {
		n, err = io.ReadFull(ps.audioSocket, data)
	} else {
		n, err = io.ReadFull(ps.controlSocket, data)
	}

	if err != nil {
		return false
	}

	if n != 64 {
		return false
	}

	ps.deviceName = string(data[:bytes.IndexByte(data, 0)])

	return true
}

func connectHandler(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")

	w.Header().Set("Cache-Control", "no-store")

	switch req.Method {
	case http.MethodOptions:
		if req.Header.Get("Access-Control-Request-Method") == "" {
			w.Header().Set("Allow", "OPTIONS, GET")
		} else if origin != "" {
			requestHeaders := req.Header.Get("Access-Control-Request-Headers")

			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET")

			if requestHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", requestHeaders)
			}
		}
	case http.MethodGet:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		var username string
		var user *User

		if len(config.Users) > 0 {
			username, user = auth(w, req)
			if user == nil {
				return
			}
			endpoint, ok := endpointMap[req.URL.Path]
			if ok {
				_, ok = endpoint[username]
				if !ok {
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}
		}

		port := getPort(req.URL.Query().Get("port"))
		if port == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if len(config.Users) > 0 && !portAllowedForUser(port, username) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		ps, ok := portMap[port]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(runCommand(ps, port, []string{"connect"}))
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func disconnectHandler(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")

	w.Header().Set("Cache-Control", "no-store")

	switch req.Method {
	case http.MethodOptions:
		if req.Header.Get("Access-Control-Request-Method") == "" {
			w.Header().Set("Allow", "OPTIONS, GET")
		} else if origin != "" {
			requestHeaders := req.Header.Get("Access-Control-Request-Headers")

			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET")

			if requestHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", requestHeaders)
			}
		}
	case http.MethodGet:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		var username string
		var user *User

		if len(config.Users) > 0 {
			username, user = auth(w, req)
			if user == nil {
				return
			}
			endpoint, ok := endpointMap[req.URL.Path]
			if ok {
				_, ok = endpoint[username]
				if !ok {
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}
		}

		port := getPort(req.URL.Query().Get("port"))
		if port == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if len(config.Users) > 0 && !portAllowedForUser(port, username) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		ps, ok := portMap[port]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(runCommand(ps, port, []string{"disconnect"}))
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func startScrcpyServerHandler(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")

	w.Header().Set("Cache-Control", "no-store")

	switch req.Method {
	case http.MethodOptions:
		if req.Header.Get("Access-Control-Request-Method") == "" {
			w.Header().Set("Allow", "OPTIONS, GET")
		} else if origin != "" {
			requestHeaders := req.Header.Get("Access-Control-Request-Headers")

			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET")

			if requestHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", requestHeaders)
			}
		}
	case http.MethodGet:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		var username string
		var user *User

		if len(config.Users) > 0 {
			username, user = auth(w, req)
			if user == nil {
				return
			}
			endpoint, ok := endpointMap[req.URL.Path]
			if ok {
				_, ok = endpoint[username]
				if !ok {
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}
		}

		port := getPort(req.URL.Query().Get("port"))
		if port == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if len(config.Users) > 0 && !portAllowedForUser(port, username) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		ps, ok := portMap[port]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(runCommand(ps, port, []string{"startscrcpyserver"}))
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func stopScrcpyServerHandler(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")

	w.Header().Set("Cache-Control", "no-store")

	switch req.Method {
	case http.MethodOptions:
		if req.Header.Get("Access-Control-Request-Method") == "" {
			w.Header().Set("Allow", "OPTIONS, GET")
		} else if origin != "" {
			requestHeaders := req.Header.Get("Access-Control-Request-Headers")

			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET")

			if requestHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", requestHeaders)
			}
		}
	case http.MethodGet:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		var username string
		var user *User

		if len(config.Users) > 0 {
			username, user = auth(w, req)
			if user == nil {
				return
			}
			endpoint, ok := endpointMap[req.URL.Path]
			if ok {
				_, ok = endpoint[username]
				if !ok {
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}
		}

		port := getPort(req.URL.Query().Get("port"))
		if port == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if len(config.Users) > 0 && !portAllowedForUser(port, username) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		ps, ok := portMap[port]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(runCommand(ps, port, []string{"stopscrcpyserver"}))
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func portInfoHandler(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")

	w.Header().Set("Cache-Control", "no-store")

	switch req.Method {
	case http.MethodOptions:
		if req.Header.Get("Access-Control-Request-Method") == "" {
			w.Header().Set("Allow", "OPTIONS, GET")
		} else if origin != "" {
			requestHeaders := req.Header.Get("Access-Control-Request-Headers")

			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET")

			if requestHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", requestHeaders)
			}
		}
	case http.MethodGet:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		var username string
		var user *User

		if len(config.Users) > 0 {
			username, user = auth(w, req)
			if user == nil {
				return
			}
			endpoint, ok := endpointMap[req.URL.Path]
			if ok {
				_, ok = endpoint[username]
				if !ok {
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}
		}

		port := getPort(req.URL.Query().Get("port"))
		if port == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if len(config.Users) > 0 && !portAllowedForUser(port, username) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		ps, ok := portMap[port]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		switch req.URL.Path {
		case "/device-name":
			if ps.deviceName == "" {
				w.WriteHeader(http.StatusNotFound)
			} else {
				w.Write([]byte(ps.deviceName))
			}
		case "/video-codec":
			if ps.videoCodec == 0 {
				w.WriteHeader(http.StatusNotFound)
			} else {
				w.Write([]byte(strconv.FormatUint(uint64(ps.videoCodec), 10)))
			}
		case "/audio-codec":
			if ps.audioCodec == 0 {
				w.WriteHeader(http.StatusNotFound)
			} else {
				w.Write([]byte(strconv.FormatUint(uint64(ps.audioCodec), 10)))
			}
		case "/initial-video-width":
			if ps.initialVideoWidth == 0 {
				w.WriteHeader(http.StatusNotFound)
			} else {
				w.Write([]byte(strconv.FormatUint(uint64(ps.initialVideoWidth), 10)))
			}
		case "/initial-video-height":
			if ps.initialVideoHeight == 0 {
				w.WriteHeader(http.StatusNotFound)
			} else {
				w.Write([]byte(strconv.FormatUint(uint64(ps.initialVideoHeight), 10)))
			}
		}
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func portsHandler(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")

	w.Header().Set("Cache-Control", "no-store")

	switch req.Method {
	case http.MethodOptions:
		if req.Header.Get("Access-Control-Request-Method") == "" {
			w.Header().Set("Allow", "OPTIONS, GET")
		} else if origin != "" {
			requestHeaders := req.Header.Get("Access-Control-Request-Headers")

			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET")

			if requestHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", requestHeaders)
			}
		}
	case http.MethodGet:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		var username string
		var user *User

		if len(config.Users) > 0 {
			username, user = auth(w, req)
			if user == nil {
				return
			}
			endpoint, ok := endpointMap[req.URL.Path]
			if ok {
				_, ok = endpoint[username]
				if !ok {
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}
		}

		ports := map[int]Port{}

		for port := range config.Ports {
			if len(config.Users) == 0 || portAllowedForUser(port, username) {
				ports[port] = config.Ports[port]
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ports)
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func sendDataHandler(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")

	w.Header().Set("Cache-Control", "no-store")

	switch req.Method {
	case http.MethodOptions:
		if req.Header.Get("Access-Control-Request-Method") == "" {
			w.Header().Set("Allow", "OPTIONS, GET")
		} else if origin != "" {
			requestHeaders := req.Header.Get("Access-Control-Request-Headers")

			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET")

			if requestHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", requestHeaders)
			}
		}
	case http.MethodGet:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		var username string
		var user *User

		if len(config.Users) > 0 {
			username, user = auth(w, req)
			if user == nil {
				return
			}
			endpoint, ok := endpointMap[req.URL.Path]
			if ok {
				_, ok = endpoint[username]
				if !ok {
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}
		}

		query := req.URL.Query()

		port := getPort(query.Get("port"))
		if port == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if len(config.Users) > 0 && !portAllowedForUser(port, username) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		ps, ok := portMap[port]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if !config.Ports[port].Control {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if ps.controlSocket == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(runCommand(ps, port, []string{"senddata", query.Get("data")}))
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func backOrScreenOnHandler(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")

	w.Header().Set("Cache-Control", "no-store")

	switch req.Method {
	case http.MethodOptions:
		if req.Header.Get("Access-Control-Request-Method") == "" {
			w.Header().Set("Allow", "OPTIONS, GET")
		} else if origin != "" {
			requestHeaders := req.Header.Get("Access-Control-Request-Headers")

			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET")

			if requestHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", requestHeaders)
			}
		}
	case http.MethodGet:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		var username string
		var user *User

		if len(config.Users) > 0 {
			username, user = auth(w, req)
			if user == nil {
				return
			}
			endpoint, ok := endpointMap[req.URL.Path]
			if ok {
				_, ok = endpoint[username]
				if !ok {
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}
		}

		query := req.URL.Query()

		port := getPort(query.Get("port"))
		if port == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if len(config.Users) > 0 && !portAllowedForUser(port, username) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		ps, ok := portMap[port]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if !config.Ports[port].Control {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if ps.controlSocket == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(runCommand(ps, port, []string{"backorscreenon"}))
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func expandNotificationsPanelHandler(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")

	w.Header().Set("Cache-Control", "no-store")

	switch req.Method {
	case http.MethodOptions:
		if req.Header.Get("Access-Control-Request-Method") == "" {
			w.Header().Set("Allow", "OPTIONS, GET")
		} else if origin != "" {
			requestHeaders := req.Header.Get("Access-Control-Request-Headers")

			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET")

			if requestHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", requestHeaders)
			}
		}
	case http.MethodGet:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		var username string
		var user *User

		if len(config.Users) > 0 {
			username, user = auth(w, req)
			if user == nil {
				return
			}
			endpoint, ok := endpointMap[req.URL.Path]
			if ok {
				_, ok = endpoint[username]
				if !ok {
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}
		}

		query := req.URL.Query()

		port := getPort(query.Get("port"))
		if port == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if len(config.Users) > 0 && !portAllowedForUser(port, username) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		ps, ok := portMap[port]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if !config.Ports[port].Control {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if ps.controlSocket == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(runCommand(ps, port, []string{"expandnotificationspanel"}))
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func expandSettingsPanelHandler(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")

	w.Header().Set("Cache-Control", "no-store")

	switch req.Method {
	case http.MethodOptions:
		if req.Header.Get("Access-Control-Request-Method") == "" {
			w.Header().Set("Allow", "OPTIONS, GET")
		} else if origin != "" {
			requestHeaders := req.Header.Get("Access-Control-Request-Headers")

			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET")

			if requestHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", requestHeaders)
			}
		}
	case http.MethodGet:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		var username string
		var user *User

		if len(config.Users) > 0 {
			username, user = auth(w, req)
			if user == nil {
				return
			}
			endpoint, ok := endpointMap[req.URL.Path]
			if ok {
				_, ok = endpoint[username]
				if !ok {
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}
		}

		query := req.URL.Query()

		port := getPort(query.Get("port"))
		if port == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if len(config.Users) > 0 && !portAllowedForUser(port, username) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		ps, ok := portMap[port]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if !config.Ports[port].Control {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if ps.controlSocket == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(runCommand(ps, port, []string{"expandsettingspanel"}))
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func collapsePanelsHandler(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")

	w.Header().Set("Cache-Control", "no-store")

	switch req.Method {
	case http.MethodOptions:
		if req.Header.Get("Access-Control-Request-Method") == "" {
			w.Header().Set("Allow", "OPTIONS, GET")
		} else if origin != "" {
			requestHeaders := req.Header.Get("Access-Control-Request-Headers")

			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET")

			if requestHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", requestHeaders)
			}
		}
	case http.MethodGet:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		var username string
		var user *User

		if len(config.Users) > 0 {
			username, user = auth(w, req)
			if user == nil {
				return
			}
			endpoint, ok := endpointMap[req.URL.Path]
			if ok {
				_, ok = endpoint[username]
				if !ok {
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}
		}

		query := req.URL.Query()

		port := getPort(query.Get("port"))
		if port == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if len(config.Users) > 0 && !portAllowedForUser(port, username) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		ps, ok := portMap[port]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if !config.Ports[port].Control {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if ps.controlSocket == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(runCommand(ps, port, []string{"collapsepanels"}))
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func turnScreenOnHandler(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")

	w.Header().Set("Cache-Control", "no-store")

	switch req.Method {
	case http.MethodOptions:
		if req.Header.Get("Access-Control-Request-Method") == "" {
			w.Header().Set("Allow", "OPTIONS, GET")
		} else if origin != "" {
			requestHeaders := req.Header.Get("Access-Control-Request-Headers")

			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET")

			if requestHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", requestHeaders)
			}
		}
	case http.MethodGet:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		var username string
		var user *User

		if len(config.Users) > 0 {
			username, user = auth(w, req)
			if user == nil {
				return
			}
			endpoint, ok := endpointMap[req.URL.Path]
			if ok {
				_, ok = endpoint[username]
				if !ok {
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}
		}

		query := req.URL.Query()

		port := getPort(query.Get("port"))
		if port == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if len(config.Users) > 0 && !portAllowedForUser(port, username) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		ps, ok := portMap[port]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if !config.Ports[port].Control {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if ps.controlSocket == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(runCommand(ps, port, []string{"turnscreenon"}))
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func turnScreenOffHandler(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")

	w.Header().Set("Cache-Control", "no-store")

	switch req.Method {
	case http.MethodOptions:
		if req.Header.Get("Access-Control-Request-Method") == "" {
			w.Header().Set("Allow", "OPTIONS, GET")
		} else if origin != "" {
			requestHeaders := req.Header.Get("Access-Control-Request-Headers")

			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET")

			if requestHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", requestHeaders)
			}
		}
	case http.MethodGet:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		var username string
		var user *User

		if len(config.Users) > 0 {
			username, user = auth(w, req)
			if user == nil {
				return
			}
			endpoint, ok := endpointMap[req.URL.Path]
			if ok {
				_, ok = endpoint[username]
				if !ok {
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}
		}

		query := req.URL.Query()

		port := getPort(query.Get("port"))
		if port == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if len(config.Users) > 0 && !portAllowedForUser(port, username) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		ps, ok := portMap[port]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if !config.Ports[port].Control {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if ps.controlSocket == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(runCommand(ps, port, []string{"turnscreenoff"}))
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func rotateHandler(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")

	w.Header().Set("Cache-Control", "no-store")

	switch req.Method {
	case http.MethodOptions:
		if req.Header.Get("Access-Control-Request-Method") == "" {
			w.Header().Set("Allow", "OPTIONS, GET")
		} else if origin != "" {
			requestHeaders := req.Header.Get("Access-Control-Request-Headers")

			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET")

			if requestHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", requestHeaders)
			}
		}
	case http.MethodGet:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		var username string
		var user *User

		if len(config.Users) > 0 {
			username, user = auth(w, req)
			if user == nil {
				return
			}
			endpoint, ok := endpointMap[req.URL.Path]
			if ok {
				_, ok = endpoint[username]
				if !ok {
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}
		}

		query := req.URL.Query()

		port := getPort(query.Get("port"))
		if port == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if len(config.Users) > 0 && !portAllowedForUser(port, username) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		ps, ok := portMap[port]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if !config.Ports[port].Control {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if ps.controlSocket == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(runCommand(ps, port, []string{"rotate"}))
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func main() {
	if len(os.Args) != 1 && len(os.Args) != 2 {
		os.Exit(1)
	}

	var configBytes []byte
	var err error

	if len(os.Args) == 1 || os.Args[1] == "-" {
		configBytes, err = io.ReadAll(os.Stdin)
	} else if strings.HasPrefix(os.Args[1], "http://") || strings.HasPrefix(os.Args[1], "https://") {
		response, err := http.Get(os.Args[1])
		if err != nil {
			panic(err)
		}

		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			os.Exit(1)
		}

		configBytes, err = io.ReadAll(response.Body)
		response.Body.Close()
	} else {
		configBytes, err = os.ReadFile(os.Args[1])
	}

	if err != nil {
		panic(err)
	}

	err = json.Unmarshal(configBytes, &config)
	if err != nil {
		panic(err)
	}

	if config.Address == "" || len(config.Ports) == 0 {
		os.Exit(1)
	}

	for port := range config.Ports {
		portMap[port] = &portState{
			connectionControlChannel:  make(chan bool),
			videoConnectedChannel:     make(chan struct{}),
			audioConnectedChannel:     make(chan struct{}),
			clipboardChannel:          make(chan string),
			uhidKeyboardOutputChannel: make(chan string),
		}

		go func(p int) {
			ps := portMap[p]
			var err error

			if !config.Ports[p].Forward {
				ps.listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
				if err != nil {
					return
				}
			}

			for connect := range ps.connectionControlChannel {
				if ps.videoSocket != nil {
					ps.videoSocket.Close()
				}

				if ps.audioSocket != nil {
					ps.audioSocket.Close()
				}

				if ps.controlSocket != nil {
					ps.controlSocket.Close()
				}

				if connect {
					if config.Ports[p].Video {
						if config.Ports[p].Forward {
							ps.videoSocket, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", p))
							if err != nil {
								return
							}

							if !readDummyByte(ps.videoSocket) {
								continue
							}
						} else {
							ps.videoSocket, err = ps.listener.Accept()
							if err != nil {
								return
							}
						}
					}

					if config.Ports[p].Audio {
						if config.Ports[p].Forward {
							ps.audioSocket, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", p))
							if err != nil {
								return
							}

							if !config.Ports[p].Video && !readDummyByte(ps.audioSocket) {
								continue
							}
						} else {
							ps.audioSocket, err = ps.listener.Accept()
							if err != nil {
								return
							}
						}
					}

					if config.Ports[p].Control {
						if config.Ports[p].Forward {
							ps.controlSocket, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", p))
							if err != nil {
								return
							}

							if !config.Ports[p].Video && !config.Ports[p].Audio && !readDummyByte(ps.controlSocket) {
								continue
							}
						} else {
							ps.controlSocket, err = ps.listener.Accept()
							if err != nil {
								return
							}
						}
					}

					if !readDeviceMeta(p) {
						continue
					}

					if config.Ports[p].Video {
						data := make([]byte, 12)
						n, err := io.ReadFull(ps.videoSocket, data)
						if err != nil {
							continue
						}
						if n != 12 {
							continue
						}

						ps.videoCodec = binary.BigEndian.Uint32(data[:4])
						ps.initialVideoWidth = binary.BigEndian.Uint32(data[4:8])
						ps.initialVideoHeight = binary.BigEndian.Uint32(data[8:])
					}

					if config.Ports[p].Audio {
						data := make([]byte, 4)
						n, err := io.ReadFull(ps.audioSocket, data)
						if err != nil {
							continue
						}
						if n != 4 {
							continue
						}

						ps.audioCodec = binary.BigEndian.Uint32(data)
					}

					if config.Ports[p].Control {
						if config.Ports[p].UhidKeyboardReportDesc != "" {
							reportDesc, err := hex.DecodeString(config.Ports[p].UhidKeyboardReportDesc)
							if err != nil {
								return
							}

							data := make([]byte, 5+len(reportDesc))
							data[0] = 0x0C
							data[2] = 1
							binary.BigEndian.PutUint16(data[3:5], uint16(len(reportDesc)))
							copy(data[5:], reportDesc)

							n, err := ps.controlSocket.Write(data)
							if err != nil {
								return
							}
							if n != len(data) {
								return
							}
						}

						if config.Ports[p].UhidMouseReportDesc != "" {
							reportDesc, err := hex.DecodeString(config.Ports[p].UhidMouseReportDesc)
							if err != nil {
								return
							}

							data := make([]byte, 5+len(reportDesc))
							data[0] = 0x0C
							data[2] = 2
							binary.BigEndian.PutUint16(data[3:5], uint16(len(reportDesc)))
							copy(data[5:], reportDesc)

							n, err := ps.controlSocket.Write(data)
							if err != nil {
								return
							}
							if n != len(data) {
								return
							}
						}

						go func() {
							data := make([]byte, 262144)

							for {
								n, err := io.ReadFull(ps.controlSocket, data[:1])
								if err != nil {
									return
								}
								if n != 1 {
									return
								}

								switch data[0] {
								case 0:
									n, err = io.ReadFull(ps.controlSocket, data[1:5])
									if err != nil {
										return
									}
									if n != 4 {
										return
									}

									clipboardLength := int(binary.BigEndian.Uint32(data[1:5]))

									n, err = io.ReadFull(ps.controlSocket, data[5:5+clipboardLength])
									if err != nil {
										return
									}
									if n != clipboardLength {
										return
									}

									lineBytes, err := json.Marshal(string(data[5 : 5+clipboardLength]))
									if err != nil {
										panic(err)
									}

									go func(line string) {
										ps.clipboardChannel <- line
									}(string(lineBytes))
								case 1:
									n, err = io.ReadFull(ps.controlSocket, data[1:9])
									if err != nil {
										return
									}
									if n != 8 {
										return
									}

									go func(line string) {
										ps.clipboardChannel <- line
									}(strconv.FormatUint(binary.BigEndian.Uint64(data[1:9]), 10))
								case 2:
									n, err = io.ReadFull(ps.controlSocket, data[1:5])
									if err != nil {
										return
									}
									if n != 4 {
										return
									}

									size := int(binary.BigEndian.Uint16(data[3:5]))

									n, err = io.ReadFull(ps.controlSocket, data[5:5+size])
									if err != nil {
										return
									}
									if n != size {
										return
									}

									if int(binary.BigEndian.Uint16(data[1:3])) == 1 {
										select {
										case ps.uhidKeyboardOutputChannel <- hex.EncodeToString(data[5 : 5+size]):
										default:
										}
									}
								}
							}
						}()
					}

					if config.Ports[p].Video {
						ps.videoConnectedChannel <- struct{}{}
					}

					if config.Ports[p].Audio {
						ps.audioConnectedChannel <- struct{}{}
					}
				}
			}
		}(port)
	}

	for k, v := range config.Endpoints {
		endpointMap[k] = map[string]struct{}{}

		for _, user := range v {
			endpointMap[k][user] = struct{}{}
		}
	}

	{
		endpoint, ok := endpointMap["/connect"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/connect", connectHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/disconnect"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/disconnect", disconnectHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/start-scrcpy-server"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/start-scrcpy-server", startScrcpyServerHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/stop-scrcpy-server"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/stop-scrcpy-server", stopScrcpyServerHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/device-name"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/device-name", portInfoHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/initial-video-width"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/initial-video-width", portInfoHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/initial-video-height"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/initial-video-height", portInfoHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/video-codec"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/video-codec", portInfoHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/audio-codec"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/audio-codec", portInfoHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/ports"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/ports", portsHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/send-data"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/send-data", sendDataHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/video"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/video", videoStreamHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/audio"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/audio", audioStreamHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/clipboard"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/clipboard", clipboardStreamHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/uhid-keyboard-output"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/uhid-keyboard-output", uhidKeyboardOutputStreamHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/key"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/key", keyHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/key-down"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/key-down", keyHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/key-up"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/key-up", keyHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/type"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/type", typeHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/touch"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/touch", touchHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/touch-down"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/touch-down", touchHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/touch-up"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/touch-up", touchHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/touch-move"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/touch-move", touchHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/mouse-click"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/mouse-click", mouseHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/mouse-down"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/mouse-down", mouseHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/mouse-up"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/mouse-up", mouseHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/mouse-move"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/mouse-move", mouseHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/scroll-left"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/scroll-left", scrollHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/scroll-right"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/scroll-right", scrollHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/scroll-up"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/scroll-up", scrollHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/scroll-down"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/scroll-down", scrollHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/open-hard-keyboard-settings"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/open-hard-keyboard-settings", openHardKeyboardSettingsHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/get-clipboard"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/get-clipboard", getClipboardHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/get-clipboard-sync"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/get-clipboard-sync", getClipboardSyncHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/set-clipboard"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/set-clipboard", setClipboardHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/set-clipboard-sync"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/set-clipboard-sync", setClipboardSyncHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/power"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/power", keyHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/sleep"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/sleep", keyHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/wake-up"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/wake-up", keyHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/back"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/back", keyHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/home"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/home", keyHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/app-switch"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/app-switch", keyHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/menu"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/menu", keyHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/assist"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/assist", keyHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/voice-assist"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/voice-assist", keyHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/all-apps"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/all-apps", keyHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/volume-up"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/volume-up", keyHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/volume-down"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/volume-down", keyHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/brightness-up"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/brightness-up", keyHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/brightness-down"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/brightness-down", keyHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/back-or-screen-on"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/back-or-screen-on", backOrScreenOnHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/expand-notifications-panel"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/expand-notifications-panel", expandNotificationsPanelHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/expand-settings-panel"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/expand-settings-panel", expandSettingsPanelHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/collapse-panels"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/collapse-panels", collapsePanelsHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/turn-screen-on"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/turn-screen-on", turnScreenOnHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/turn-screen-off"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/turn-screen-off", turnScreenOffHandler)
		}
	}

	{
		endpoint, ok := endpointMap["/rotate"]
		if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
			http.HandleFunc("/rotate", rotateHandler)
		}
	}

	loadExtensions()

	if config.Static != "" {
		http.Handle("/", http.FileServer(http.Dir(config.Static)))
	}

	if config.Cert == "" && config.Key == "" {
		http.ListenAndServe(config.Address, nil)
	} else {
		http.ListenAndServeTLS(config.Address, config.Cert, config.Key, nil)
	}
}
