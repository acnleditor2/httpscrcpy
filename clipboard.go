package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func getClipboard(ps *portState, cut bool) bool {
	data := make([]byte, 2)
	data[0] = 0x08
	if cut {
		data[1] = 0x02
	} else {
		data[1] = 0x01
	}

	n, err := ps.controlSocket.Write(data)
	if err != nil {
		return false
	}
	if n != 2 {
		return false
	}

	return true
}

func setClipboard(ps *portState, text string, sequence uint64, paste bool) bool {
	data := make([]byte, 14+len(text))
	data[0] = 0x09
	binary.BigEndian.PutUint64(data[1:], sequence)
	if paste {
		data[9] = 0x01
	}
	binary.BigEndian.PutUint32(data[10:], uint32(len(text)))
	copy(data[14:], []byte(text))

	n, err := ps.controlSocket.Write(data)
	if err != nil {
		return false
	}
	if n != len(data) {
		return false
	}

	return true
}

func getClipboardHandler(w http.ResponseWriter, req *http.Request) {
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

		w.WriteHeader(runCommand(ps, port, []string{"getclipboard", query.Get("cut")}))
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func getClipboardSyncHandler(w http.ResponseWriter, req *http.Request) {
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

		timeout, err := time.ParseDuration(query.Get("timeout"))
		if err != nil || timeout < 1 {
			w.WriteHeader(http.StatusBadRequest)
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

		if config.Ports[port].ClipboardStreamExtension != "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if ps.controlSocket == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		status := runCommand(ps, port, []string{"getclipboard", query.Get("cut")})

		if status != http.StatusNoContent {
			w.WriteHeader(status)
			return
		}

		select {
		case s := <-ps.clipboardChannel:
			if strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"") {
				w.Write([]byte(s))
			} else {
				w.WriteHeader(http.StatusInternalServerError)
			}
		case <-time.After(timeout):
			w.WriteHeader(http.StatusInternalServerError)
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

func setClipboardHandler(w http.ResponseWriter, req *http.Request) {
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

		paste := false
		var err error

		if query.Has("paste") {
			paste, err = strconv.ParseBool(query.Get("paste"))
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
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

		text := query.Get("text")
		sequence := query.Get("sequence")

		if paste {
			w.WriteHeader(runCommand(ps, port, []string{"setclipboardpaste", text, sequence}))
		} else {
			w.WriteHeader(runCommand(ps, port, []string{"setclipboard", text, sequence}))
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

func setClipboardSyncHandler(w http.ResponseWriter, req *http.Request) {
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

		paste := false
		var err error

		if query.Has("paste") {
			paste, err = strconv.ParseBool(query.Get("paste"))
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}

		sequence := query.Get("sequence")
		if sequence == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		timeout, err := time.ParseDuration(query.Get("timeout"))
		if err != nil || timeout < 1 {
			w.WriteHeader(http.StatusBadRequest)
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

		if config.Ports[port].ClipboardStreamExtension != "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if ps.controlSocket == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		text := query.Get("text")
		var status int

		if paste {
			status = runCommand(ps, port, []string{"setclipboardpaste", text, sequence})
		} else {
			status = runCommand(ps, port, []string{"setclipboard", text, sequence})
		}

		if status != http.StatusNoContent {
			w.WriteHeader(status)
			return
		}

		select {
		case s := <-ps.clipboardChannel:
			if s == sequence {
				w.Header().Set("Content-Type", "application/json")

				json.NewEncoder(w).Encode(struct {
					Sequence string `json:"sequence"`
				}{
					Sequence: s,
				})
			} else {
				w.WriteHeader(http.StatusInternalServerError)
			}
		case <-time.After(timeout):
			w.WriteHeader(http.StatusInternalServerError)
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

func clipboardStreamHandler(w http.ResponseWriter, req *http.Request) {
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

		if !config.Ports[port].Control {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if config.Ports[port].ClipboardStreamExtension != "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if ps.controlSocket == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var err error

		for {
			select {
			case line := <-ps.clipboardChannel:
				_, err = fmt.Fprintln(w, line)
				if err != nil {
					return
				}

				w.(http.Flusher).Flush()
			case <-req.Context().Done():
				return
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

func sendClipboardToExtension(port int, ps *portState, extension *extensionState) {
	for {
		s := <-ps.clipboardChannel

		var b bytes.Buffer

		if strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"") {
			var text string
			err := json.Unmarshal([]byte(s), &text)
			if err != nil {
				ps.connectionControlChannel <- false
				break
			}

			b.WriteByte(5)
			binary.Write(&b, binary.NativeEndian, uint16(port))
			binary.Write(&b, binary.NativeEndian, uint32(len(text)))
			b.WriteString(text)
		} else {
			sequence, err := strconv.ParseUint(s, 10, 64)
			if err != nil {
				ps.connectionControlChannel <- false
				break
			}

			b.WriteByte(6)
			binary.Write(&b, binary.NativeEndian, uint16(port))
			binary.Write(&b, binary.NativeEndian, sequence)
		}

		extension.mutex.Lock()
		_, err := b.WriteTo(extension.stdin)
		extension.mutex.Unlock()
		if err != nil {
			ps.connectionControlChannel <- false
			break
		}
	}
}
