package main

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func runCommand(ps *portState, port int, command []string) int {
	switch command[0] {
	case "connect":
		select {
		case ps.connectionControlChannel <- true:
			return http.StatusNoContent
		default:
			return http.StatusServiceUnavailable
		}
	case "disconnect":
		select {
		case ps.connectionControlChannel <- false:
			return http.StatusNoContent
		default:
			return http.StatusServiceUnavailable
		}
	case "key", "key2":
		if len(command) == 2 || len(command) == 3 {
			var keycode int
			var err error

			if command[0] == "key" {
				keycode = keycodeMap[strings.ToLower(command[1])]
				if keycode == 0 {
					return http.StatusNotFound
				}
			} else {
				keycode, err = strconv.Atoi(command[1])
				if err != nil {
					return http.StatusBadRequest
				}
			}

			var duration time.Duration

			if len(command) == 3 {
				duration, err = time.ParseDuration(command[2])
				if err != nil {
					return http.StatusBadRequest
				}
			}

			if !sendKeyEvent(ps, false, keycode, 0, 0) {
				return http.StatusInternalServerError
			}

			if duration > 0 {
				time.Sleep(duration)
			}

			if !sendKeyEvent(ps, true, keycode, 0, 0) {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusBadRequest
		}
	case "type", "typebase64", "typebase64url", "typehex":
		if len(command) == 2 {
			if command[1] == "" {
				return http.StatusBadRequest
			}

			var text string

			if command[0] == "typebase64" {
				textBytes, err := base64.StdEncoding.DecodeString(command[1])
				if err != nil {
					return http.StatusBadRequest
				}
				text = string(textBytes)
			} else if command[0] == "typebase64url" {
				textBytes, err := base64.URLEncoding.DecodeString(command[1])
				if err != nil {
					return http.StatusBadRequest
				}
				text = string(textBytes)
			} else if command[0] == "typehex" {
				textBytes, err := hex.DecodeString(command[1])
				if err != nil {
					return http.StatusBadRequest
				}
				text = string(textBytes)
			} else {
				text = command[1]
			}

			data := make([]byte, 5+len(text))
			data[0] = 0x01
			binary.BigEndian.PutUint32(data[1:5], uint32(len(text)))
			copy(data[5:], []byte(text))

			n, err := ps.controlSocket.Write(data)
			if err != nil {
				return http.StatusInternalServerError
			}
			if n != len(data) {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusBadRequest
		}
	case "touch":
		if len(command) == 5 || len(command) == 6 {
			x, err := strconv.Atoi(command[1])
			if err != nil {
				return http.StatusBadRequest
			}

			y, err := strconv.Atoi(command[2])
			if err != nil {
				return http.StatusBadRequest
			}

			width, err := strconv.Atoi(command[3])
			if err != nil {
				return http.StatusBadRequest
			}

			height, err := strconv.Atoi(command[4])
			if err != nil {
				return http.StatusBadRequest
			}

			var duration time.Duration

			if len(command) == 6 && command[5] != "" {
				duration, err = time.ParseDuration(command[5])
				if err != nil {
					return http.StatusBadRequest
				}
			}

			if !sendTouchEvent(ps, 0, x, y, width, height) {
				return http.StatusInternalServerError
			}

			if duration > 0 {
				time.Sleep(duration)
			}

			if !sendTouchEvent(ps, 1, x, y, width, height) {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusBadRequest
		}
	case "touchdown":
		if len(command) == 5 {
			x, err := strconv.Atoi(command[1])
			if err != nil {
				return http.StatusBadRequest
			}

			y, err := strconv.Atoi(command[2])
			if err != nil {
				return http.StatusBadRequest
			}

			width, err := strconv.Atoi(command[3])
			if err != nil {
				return http.StatusBadRequest
			}

			height, err := strconv.Atoi(command[4])
			if err != nil {
				return http.StatusBadRequest
			}

			if !sendTouchEvent(ps, 0, x, y, width, height) {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusBadRequest
		}
	case "touchup":
		if len(command) == 5 {
			x, err := strconv.Atoi(command[1])
			if err != nil {
				return http.StatusBadRequest
			}

			y, err := strconv.Atoi(command[2])
			if err != nil {
				return http.StatusBadRequest
			}

			width, err := strconv.Atoi(command[3])
			if err != nil {
				return http.StatusBadRequest
			}

			height, err := strconv.Atoi(command[4])
			if err != nil {
				return http.StatusBadRequest
			}

			if !sendTouchEvent(ps, 1, x, y, width, height) {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusBadRequest
		}
	case "touchmove":
		if len(command) == 5 {
			x, err := strconv.Atoi(command[1])
			if err != nil {
				return http.StatusBadRequest
			}

			y, err := strconv.Atoi(command[2])
			if err != nil {
				return http.StatusBadRequest
			}

			width, err := strconv.Atoi(command[3])
			if err != nil {
				return http.StatusBadRequest
			}

			height, err := strconv.Atoi(command[4])
			if err != nil {
				return http.StatusBadRequest
			}

			if !sendTouchEvent(ps, 2, x, y, width, height) {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusBadRequest
		}
	case "mouseclick":
		if len(command) == 6 || len(command) == 7 {
			button := getButton(strings.ToLower(command[1]))
			if button == -1 {
				return http.StatusBadRequest
			}

			x, err := strconv.Atoi(command[2])
			if err != nil {
				return http.StatusBadRequest
			}

			y, err := strconv.Atoi(command[3])
			if err != nil {
				return http.StatusBadRequest
			}

			width, err := strconv.Atoi(command[4])
			if err != nil {
				return http.StatusBadRequest
			}

			height, err := strconv.Atoi(command[5])
			if err != nil {
				return http.StatusBadRequest
			}

			var duration time.Duration
			if len(command) == 7 && command[6] != "" {
				duration, err = time.ParseDuration(command[6])
				if err != nil {
					return http.StatusBadRequest
				}
			}

			if !sendMouseEvent(ps, 0, x, y, width, height, button) {
				return http.StatusInternalServerError
			}

			if duration > 0 {
				time.Sleep(duration)
			}

			if !sendMouseEvent(ps, 1, x, y, width, height, button) {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusBadRequest
		}
	case "mousedown":
		if len(command) == 6 {
			button := getButton(strings.ToLower(command[1]))
			if button == -1 {
				return http.StatusBadRequest
			}

			x, err := strconv.Atoi(command[1])
			if err != nil {
				return http.StatusBadRequest
			}

			y, err := strconv.Atoi(command[2])
			if err != nil {
				return http.StatusBadRequest
			}

			width, err := strconv.Atoi(command[3])
			if err != nil {
				return http.StatusBadRequest
			}

			height, err := strconv.Atoi(command[4])
			if err != nil {
				return http.StatusBadRequest
			}

			if !sendMouseEvent(ps, 0, x, y, width, height, button) {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusBadRequest
		}
	case "mouseup":
		if len(command) == 6 {
			button := getButton(strings.ToLower(command[1]))
			if button == -1 {
				return http.StatusBadRequest
			}

			x, err := strconv.Atoi(command[1])
			if err != nil {
				return http.StatusBadRequest
			}

			y, err := strconv.Atoi(command[2])
			if err != nil {
				return http.StatusBadRequest
			}

			width, err := strconv.Atoi(command[3])
			if err != nil {
				return http.StatusBadRequest
			}

			height, err := strconv.Atoi(command[4])
			if err != nil {
				return http.StatusBadRequest
			}

			if !sendMouseEvent(ps, 1, x, y, width, height, button) {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusBadRequest
		}
	case "mousemove":
		if len(command) == 6 {
			button := getButton(strings.ToLower(command[1]))
			if button == -1 {
				return http.StatusBadRequest
			}

			x, err := strconv.Atoi(command[1])
			if err != nil {
				return http.StatusBadRequest
			}

			y, err := strconv.Atoi(command[2])
			if err != nil {
				return http.StatusBadRequest
			}

			width, err := strconv.Atoi(command[3])
			if err != nil {
				return http.StatusBadRequest
			}

			height, err := strconv.Atoi(command[4])
			if err != nil {
				return http.StatusBadRequest
			}

			if !sendMouseEvent(ps, 2, x, y, width, height, button) {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusBadRequest
		}
	case "scrollleft", "scrollright", "scrollup", "scrolldown":
		if len(command) == 5 {
			x, err := strconv.Atoi(command[1])
			if err != nil {
				return http.StatusBadRequest
			}

			y, err := strconv.Atoi(command[2])
			if err != nil {
				return http.StatusBadRequest
			}

			width, err := strconv.Atoi(command[3])
			if err != nil {
				return http.StatusBadRequest
			}

			height, err := strconv.Atoi(command[4])
			if err != nil {
				return http.StatusBadRequest
			}

			if !sendScrollEvent(ps, command[0][6:], x, y, width, height) {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusBadRequest
		}
	case "backorscreenon":
		if len(command) == 1 {
			n, err := ps.controlSocket.Write([]byte{0x04, 0x00, 0x04, 0x01})
			if err != nil {
				return http.StatusInternalServerError
			}
			if n != 4 {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusBadRequest
		}
	case "expandnotificationspanel":
		if len(command) == 1 {
			n, err := ps.controlSocket.Write([]byte{0x05})
			if err != nil {
				return http.StatusInternalServerError
			}
			if n != 1 {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusBadRequest
		}
	case "expandsettingspanel":
		if len(command) == 1 {
			n, err := ps.controlSocket.Write([]byte{0x06})
			if err != nil {
				return http.StatusInternalServerError
			}
			if n != 1 {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusBadRequest
		}
	case "collapsepanels":
		if len(command) == 1 {
			n, err := ps.controlSocket.Write([]byte{0x07})
			if err != nil {
				return http.StatusInternalServerError
			}
			if n != 1 {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusBadRequest
		}
	case "getclipboard":
		if len(command) == 1 {
			if !getClipboard(ps, false) {
				return http.StatusInternalServerError
			}
		} else if len(command) == 2 {
			cut, err := strconv.ParseBool(command[1])
			if err != nil {
				return http.StatusBadRequest
			}

			if !getClipboard(ps, cut) {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusBadRequest
		}
	case "setclipboard", "setclipboardbase64", "setclipboardbase64url", "setclipboardhex", "setclipboardpaste", "setclipboardpastebase64", "setclipboardpastebase64url", "setclipboardpastehex":
		if len(command) == 2 || len(command) == 3 {
			var text string

			if strings.HasSuffix(command[0], "base64") {
				decoded, err := base64.StdEncoding.DecodeString(command[1])
				if err != nil {
					return http.StatusBadRequest
				}
				text = string(decoded)
			} else if strings.HasSuffix(command[0], "base64url") {
				decoded, err := base64.URLEncoding.DecodeString(command[1])
				if err != nil {
					return http.StatusBadRequest
				}
				text = string(decoded)
			} else if strings.HasSuffix(command[0], "hex") {
				decoded, err := hex.DecodeString(command[1])
				if err != nil {
					return http.StatusBadRequest
				}
				text = string(decoded)
			} else {
				text = command[1]
			}

			var sequence uint64
			var err error

			if len(command) == 3 && command[2] != "" {
				sequence, err = strconv.ParseUint(command[2], 10, 64)
				if err != nil {
					return http.StatusBadRequest
				}
			}

			if !setClipboard(ps, text, sequence, strings.HasPrefix(command[0], "setclipboardpaste")) {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusBadRequest
		}
	case "turnscreenon":
		if len(command) == 1 {
			n, err := ps.controlSocket.Write([]byte{0x0A, 0x02})
			if err != nil {
				return http.StatusInternalServerError
			}
			if n != 2 {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusBadRequest
		}
	case "turnscreenoff":
		if len(command) == 1 {
			n, err := ps.controlSocket.Write([]byte{0x0A, 0x00})
			if err != nil {
				return http.StatusInternalServerError
			}
			if n != 2 {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusBadRequest
		}
	case "rotate":
		if len(command) == 1 {
			n, err := ps.controlSocket.Write([]byte{0x0B})
			if err != nil {
				return http.StatusInternalServerError
			}
			if n != 1 {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusBadRequest
		}
	case "senddata":
		if len(command) == 2 {
			data, err := hex.DecodeString(command[1])
			if err != nil {
				return http.StatusBadRequest
			}
			if len(data) == 0 {
				return http.StatusBadRequest
			}

			n, err := ps.controlSocket.Write(data)
			if err != nil {
				return http.StatusInternalServerError
			}
			if n != len(data) {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusBadRequest
		}
	case "scriptmessage":
		if len(command) == 3 {
			scriptMessageChannel, ok := scriptMessageChannelMap[command[1]]
			if !ok {
				return http.StatusNotFound
			}

			go func(message string) {
				scriptMessageChannel <- message
			}(command[2])
		} else {
			return http.StatusBadRequest
		}
	default:
		return http.StatusNotFound
	}

	return http.StatusNoContent
}
