package main

import (
	"encoding/binary"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var keycodeMap = map[string]int{
	"0":              7,
	"1":              8,
	"2":              9,
	"3":              10,
	"4":              11,
	"5":              12,
	"6":              13,
	"7":              14,
	"8":              15,
	"9":              16,
	"a":              29,
	"b":              30,
	"c":              31,
	"d":              32,
	"e":              33,
	"f":              34,
	"g":              35,
	"h":              36,
	"i":              37,
	"j":              38,
	"k":              39,
	"l":              40,
	"m":              41,
	"n":              42,
	"o":              43,
	"p":              44,
	"q":              45,
	"r":              46,
	"s":              47,
	"t":              48,
	"u":              49,
	"v":              50,
	"w":              51,
	"x":              52,
	"y":              53,
	"z":              54,
	" ":              62,
	"#":              18,
	"'":              75,
	"(":              162,
	")":              163,
	"*":              17,
	"+":              81,
	",":              55,
	"-":              69,
	".":              56,
	"/":              76,
	";":              74,
	"=":              70,
	"@":              77,
	"[":              71,
	"\\":             73,
	"]":              72,
	"`":              68,
	"\n":             66,
	"\t":             61,
	"home":           3,
	"back":           4,
	"up":             19,
	"down":           20,
	"left":           21,
	"right":          22,
	"volumeup":       24,
	"volumedown":     25,
	"power":          26,
	"backspace":      67,
	"menu":           82,
	"mediaplaypause": 85,
	"mediastop":      86,
	"medianext":      87,
	"mediaprevious":  88,
	"pageup":         92,
	"pagedown":       93,
	"escape":         111,
	"delete":         112,
	"movehome":       122,
	"moveend":        123,
	"insert":         124,
	"numpad0":        144,
	"numpad1":        145,
	"numpad2":        146,
	"numpad3":        147,
	"numpad4":        148,
	"numpad5":        149,
	"numpad6":        150,
	"numpad7":        151,
	"numpad8":        152,
	"numpad9":        153,
	"numpaddivide":   154,
	"numpadmultiply": 155,
	"numpadsubtract": 156,
	"numpadadd":      157,
	"numpaddot":      158,
	"numpadenter":    160,
	"numpadequals":   161,
	"appswitch":      187,
	"assist":         219,
	"brightnessdown": 220,
	"brightnessup":   221,
	"sleep":          223,
	"wakeup":         224,
	"voiceassist":    231,
	"allapps":        284,
}

func sendKeyEvent(ps *portState, up bool, keycode int, repeat int, metaState int) bool {
	data := make([]byte, 14)
	if up {
		data[1] = 0x01
	}
	binary.BigEndian.PutUint32(data[2:6], uint32(keycode))
	binary.BigEndian.PutUint32(data[6:10], uint32(repeat))
	binary.BigEndian.PutUint32(data[10:], uint32(metaState))

	n, err := ps.controlSocket.Write(data)
	if err != nil {
		return false
	}
	if n != 14 {
		return false
	}

	return true
}

func sendTouchEvent(ps *portState, action int, x int, y int, width int, height int) bool {
	data := make([]byte, 32)
	data[0] = 0x02
	data[1] = byte(action)
	data[2] = 0xff
	data[3] = 0xff
	data[4] = 0xff
	data[5] = 0xff
	data[6] = 0xff
	data[7] = 0xff
	data[8] = 0xff
	data[9] = 0xfe
	binary.BigEndian.PutUint32(data[10:], uint32(x))
	binary.BigEndian.PutUint32(data[14:], uint32(y))
	binary.BigEndian.PutUint16(data[18:], uint16(width))
	binary.BigEndian.PutUint16(data[20:], uint16(height))
	if action != 1 {
		data[22] = 0xff
		data[23] = 0xff
	}
	data[27] = 0x01
	if action != 1 {
		data[31] = 0x01
	}

	n, err := ps.controlSocket.Write(data)
	if err != nil {
		return false
	}
	if n != 32 {
		return false
	}

	return true
}

func sendMouseEvent(ps *portState, action int, x int, y int, width int, height int, button int) bool {
	data := make([]byte, 32)
	data[0] = 0x02
	data[1] = byte(action)
	data[2] = 0xff
	data[3] = 0xff
	data[4] = 0xff
	data[5] = 0xff
	data[6] = 0xff
	data[7] = 0xff
	data[8] = 0xff
	data[9] = 0xff
	binary.BigEndian.PutUint32(data[10:], uint32(x))
	binary.BigEndian.PutUint32(data[14:], uint32(y))
	binary.BigEndian.PutUint16(data[18:], uint16(width))
	binary.BigEndian.PutUint16(data[20:], uint16(height))
	if action != 1 {
		data[22] = 0xff
		data[23] = 0xff
	}
	binary.BigEndian.PutUint32(data[24:], uint32(button))
	if action != 1 {
		binary.BigEndian.PutUint32(data[28:], uint32(button))
	}

	n, err := ps.controlSocket.Write(data)
	if err != nil {
		return false
	}
	if n != 32 {
		return false
	}

	return true
}

func sendScrollEvent(ps *portState, direction string, x int, y int, width int, height int) bool {
	data := make([]byte, 21)
	data[0] = 0x03
	binary.BigEndian.PutUint32(data[1:], uint32(x))
	binary.BigEndian.PutUint32(data[5:], uint32(y))
	binary.BigEndian.PutUint16(data[9:], uint16(width))
	binary.BigEndian.PutUint16(data[11:], uint16(height))
	switch direction {
	case "left":
		data[13] = 0x80
	case "right":
		data[13] = 0x7F
		data[14] = 0xFF
	case "up":
		data[15] = 0x7F
		data[16] = 0xFF
	case "down":
		data[15] = 0x80
	}

	n, err := ps.controlSocket.Write(data)
	if err != nil {
		return false
	}
	if n != 21 {
		return false
	}

	return true
}

func getButton(buttonString string) int {
	switch buttonString {
	case "", "left":
		return 1
	case "right":
		return 2
	case "middle":
		return 4
	}

	button, err := strconv.Atoi(buttonString)
	if err != nil {
		return -1
	}

	return button
}

func keyHandler(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")

	w.Header().Set("Cache-Control", "no-store")

	switch req.Method {
	case http.MethodOptions:
		if req.Header.Get("Access-Control-Request-Method") == "" {
			w.Header().Set("Allow", "OPTIONS, GET")
		} else if origin != "" {
			requestHeaders := req.Header.Get("Access-Control-Request-Headers")

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
		if port == -1 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if len(config.Users) > 0 && !portAllowedForUser(port, username) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		keycode := 0
		keyInPath := !strings.HasPrefix(req.URL.Path, "/key")
		var err error

		if keyInPath {
			keycode = keycodeMap[strings.ReplaceAll(req.URL.Path[1:], "-", "")]
		} else if query.Has("keycode") {
			keycode, err = strconv.Atoi(query.Get("keycode"))
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		} else {
			keycode = keycodeMap[strings.ToLower(query.Get("key"))]
			if keycode == 0 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}

		repeat := 0

		if !keyInPath && query.Has("repeat") {
			repeat, err = strconv.Atoi(query.Get("repeat"))
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}

		metaState := 0

		if !keyInPath && query.Has("metastate") {
			metaState, err = strconv.Atoi(query.Get("metastate"))
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}

		var duration time.Duration

		if (keyInPath || req.URL.Path == "/key") && query.Has("duration") {
			duration, err = time.ParseDuration(query.Get("duration"))
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

		if req.URL.Path != "/key-up" {
			if !sendKeyEvent(ps, false, keycode, repeat, metaState) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}

		if duration > 0 {
			time.Sleep(duration)
		}

		if req.URL.Path != "/key-down" {
			if !sendKeyEvent(ps, true, keycode, repeat, metaState) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}

		w.WriteHeader(http.StatusNoContent)
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func typeHandler(w http.ResponseWriter, req *http.Request) {
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

		w.WriteHeader(runCommand(ps, port, []string{"type", query.Get("text")}))
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func touchHandler(w http.ResponseWriter, req *http.Request) {
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

		x := query.Get("x")
		y := query.Get("y")
		width := query.Get("width")
		height := query.Get("height")

		if req.URL.Path == "/touch" {
			w.WriteHeader(runCommand(ps, port, []string{"touch", x, y, width, height, query.Get("duration")}))
		} else {
			w.WriteHeader(runCommand(ps, port, []string{"touch" + req.URL.Path[7:], x, y, width, height}))
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

func mouseHandler(w http.ResponseWriter, req *http.Request) {
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

		button := query.Get("button")
		x := query.Get("x")
		y := query.Get("y")
		width := query.Get("width")
		height := query.Get("height")

		if req.URL.Path == "/mouse-click" {
			w.WriteHeader(runCommand(ps, port, []string{"mouseclick", button, x, y, width, height, query.Get("duration")}))
		} else {
			w.WriteHeader(runCommand(ps, port, []string{"mouse" + req.URL.Path[7:], button, x, y, width, height}))
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

func scrollHandler(w http.ResponseWriter, req *http.Request) {
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

		w.WriteHeader(runCommand(ps, port, []string{"scroll" + req.URL.Path[8:], query.Get("x"), query.Get("y"), query.Get("width"), query.Get("height")}))
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
