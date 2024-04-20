package main

import (
	"encoding/binary"
	"fmt"
	"net/http"
	"strconv"
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
	if n != 2 {
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
			if user.ScriptOnly {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}

		query := req.URL.Query()

		port := getPort(query.Get("port"))
		if port == -1 {
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

		if !ps.control || ps.controlSocket == nil {
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
			if user.ScriptOnly {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}

		query := req.URL.Query()

		port := getPort(query.Get("port"))
		if port == -1 {
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

		if !ps.control || ps.controlSocket == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		text := query.Get("text")

		if paste {
			w.WriteHeader(runCommand(ps, port, []string{"setclipboardpaste", text, query.Get("sequence")}))
		} else {
			w.WriteHeader(runCommand(ps, port, []string{"setclipboard", text, query.Get("sequence")}))
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
			if user.ScriptOnly {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}

		port := getPort(req.URL.Query().Get("port"))
		if port == -1 {
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
