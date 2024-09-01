package main

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func runCommand(ps *portState, port int, command []string) int {
	switch command[0] {
	case "connect":
		if len(command) == 1 {
			select {
			case ps.connectionControlChannel <- true:
				return http.StatusNoContent
			default:
				return http.StatusServiceUnavailable
			}
		} else {
			return http.StatusNotFound
		}
	case "disconnect":
		if len(command) == 1 {
			if ps.scrcpyServer != nil {
				return http.StatusServiceUnavailable
			}

			select {
			case ps.connectionControlChannel <- false:
				return http.StatusNoContent
			default:
				return http.StatusServiceUnavailable
			}
		} else {
			return http.StatusNotFound
		}
	case "startscrcpyserver":
		if len(command) == 1 {
			if len(config.Ports[port].ADB) == 0 {
				return http.StatusNotFound
			}

			if len(config.Ports[port].ScrcpyServer) != 2 {
				return http.StatusNotFound
			}

			if ps.scrcpyServer != nil {
				select {
				case ps.connectionControlChannel <- false:
					time.Sleep(1 * time.Second)
				default:
				}

				ps.scrcpyServer.Process.Kill()
				ps.scrcpyServer.Wait()
			}

			args := append(
				config.Ports[port].ADB[1:],
				"shell",
				fmt.Sprintf("CLASSPATH=%s", config.Ports[port].ScrcpyServer[0]),
				"app_process",
				"/",
				"com.genymobile.scrcpy.Server",
				config.Ports[port].ScrcpyServer[1],
			)

			if !config.Ports[port].Video {
				args = append(args, "video=false")
			}

			if !config.Ports[port].Audio {
				args = append(args, "audio=false")
			}

			if config.Ports[port].Control {
				if !config.Ports[port].ClipboardAutosync {
					args = append(args, "clipboard_autosync=false")
				}
			} else {
				args = append(args, "control=false")
			}

			if !config.Ports[port].Cleanup {
				args = append(args, "cleanup=false")
			}

			if !config.Ports[port].PowerOn {
				args = append(args, "power_on=false")
			}

			if config.Ports[port].Forward {
				args = append(args, "tunnel_forward=true")
			}

			if len(config.Ports[port].ScrcpyServerOptions) > 0 {
				args = append(args, config.Ports[port].ScrcpyServerOptions...)
			}

			ps.scrcpyServer = exec.Command(config.Ports[port].ADB[0], args...)
			ps.scrcpyServer.Stdin = nil
			ps.scrcpyServer.Stdout = os.Stdout
			ps.scrcpyServer.Stderr = os.Stderr

			if ps.scrcpyServer.Start() != nil {
				ps.scrcpyServer = nil
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusNotFound
		}
	case "stopscrcpyserver":
		if len(command) == 1 {
			if ps.scrcpyServer == nil {
				return http.StatusNotFound
			}

			select {
			case ps.connectionControlChannel <- false:
				time.Sleep(1 * time.Second)
			default:
			}

			ps.scrcpyServer.Process.Kill()
			ps.scrcpyServer.Wait()
			ps.scrcpyServer = nil
		} else {
			return http.StatusNotFound
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

			if !sendKeyEventSdk(ps, false, keycode, 0, 0) {
				return http.StatusInternalServerError
			}

			if duration > 0 {
				time.Sleep(duration)
			}

			if !sendKeyEventSdk(ps, true, keycode, 0, 0) {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusNotFound
		}
	case "key3":
		if (len(command) == 2 || len(command) == 3) && config.Ports[port].UhidKeyboardReportDesc != "" {
			scancode, err := strconv.Atoi(command[1])
			if err != nil {
				return http.StatusBadRequest
			}

			var duration time.Duration

			if len(command) == 3 {
				duration, err = time.ParseDuration(command[2])
				if err != nil {
					return http.StatusBadRequest
				}
			}

			if !sendKeyEventUhid(ps, scancode, 0) {
				return http.StatusInternalServerError
			}

			if duration > 0 {
				time.Sleep(duration)
			}

			if !sendKeyEventUhid(ps, 0, 0) {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusNotFound
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
			return http.StatusNotFound
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
			return http.StatusNotFound
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
			return http.StatusNotFound
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
			return http.StatusNotFound
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
			return http.StatusNotFound
		}
	case "mouseclick":
		if (len(command) == 4 || len(command) == 5) && config.Ports[port].UhidMouseReportDesc != "" {
			x, err := strconv.Atoi(command[2])
			if err != nil {
				return http.StatusBadRequest
			}

			y, err := strconv.Atoi(command[3])
			if err != nil {
				return http.StatusBadRequest
			}

			var duration time.Duration
			if len(command) == 5 && command[4] != "" {
				duration, err = time.ParseDuration(command[4])
				if err != nil {
					return http.StatusBadRequest
				}
			}

			button := getButton(command[1])

			if !sendMouseEventUhid(ps, x, y, button) {
				return http.StatusInternalServerError
			}

			if duration > 0 {
				time.Sleep(duration)
			}

			if !sendMouseEventUhid(ps, 0, 0, 0) {
				return http.StatusInternalServerError
			}
		} else if len(command) == 6 || len(command) == 7 {
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

			button := getButton(command[1])

			if !sendMouseEventSdk(ps, 0, x, y, width, height, button) {
				return http.StatusInternalServerError
			}

			if duration > 0 {
				time.Sleep(duration)
			}

			if !sendMouseEventSdk(ps, 1, x, y, width, height, button) {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusNotFound
		}
	case "mousedown":
		if len(command) == 4 && config.Ports[port].UhidMouseReportDesc != "" {
			x, err := strconv.Atoi(command[2])
			if err != nil {
				return http.StatusBadRequest
			}

			y, err := strconv.Atoi(command[3])
			if err != nil {
				return http.StatusBadRequest
			}

			if !sendMouseEventUhid(ps, x, y, getButton(command[1])) {
				return http.StatusInternalServerError
			}
		} else if len(command) == 6 {
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

			if !sendMouseEventSdk(ps, 0, x, y, width, height, getButton(command[1])) {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusNotFound
		}
	case "mouseup":
		if len(command) == 1 && config.Ports[port].UhidMouseReportDesc != "" {
			if !sendMouseEventUhid(ps, 0, 0, 0) {
				return http.StatusInternalServerError
			}
		} else if len(command) == 6 && config.Ports[port].UhidMouseReportDesc != "" {
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

			if !sendMouseEventSdk(ps, 1, x, y, width, height, getButton(command[1])) {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusNotFound
		}
	case "mousemove":
		if len(command) == 3 && config.Ports[port].UhidMouseReportDesc != "" {
			x, err := strconv.Atoi(command[1])
			if err != nil {
				return http.StatusBadRequest
			}

			y, err := strconv.Atoi(command[2])
			if err != nil {
				return http.StatusBadRequest
			}

			if !sendMouseEventUhid(ps, x, y, 0) {
				return http.StatusInternalServerError
			}
		} else if len(command) == 4 && config.Ports[port].UhidMouseReportDesc != "" {
			x, err := strconv.Atoi(command[2])
			if err != nil {
				return http.StatusBadRequest
			}

			y, err := strconv.Atoi(command[3])
			if err != nil {
				return http.StatusBadRequest
			}

			if !sendMouseEventUhid(ps, x, y, getButton(command[1])) {
				return http.StatusInternalServerError
			}
		} else if len(command) == 6 {
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

			if !sendMouseEventSdk(ps, 2, x, y, width, height, getButton(command[1])) {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusNotFound
		}
	case "scrollleft", "scrollright", "scrollup", "scrolldown":
		if len(command) == 3 && config.Ports[port].UhidMouseReportDesc != "" && (command[0] == "scrollup" || command[0] == "scrolldown") {
			x, err := strconv.Atoi(command[1])
			if err != nil {
				return http.StatusBadRequest
			}

			y, err := strconv.Atoi(command[2])
			if err != nil {
				return http.StatusBadRequest
			}

			if !sendScrollEventUhid(ps, command[0][6:], x, y) {
				return http.StatusInternalServerError
			}
		} else if len(command) == 5 {
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

			if !sendScrollEventSdk(ps, command[0][6:], x, y, width, height) {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusNotFound
		}
	case "openhardkeyboardsettings":
		if len(command) == 1 {
			n, err := ps.controlSocket.Write([]byte{0x0E})
			if err != nil {
				return http.StatusInternalServerError
			}
			if n != 1 {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusNotFound
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
			return http.StatusNotFound
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
			return http.StatusNotFound
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
			return http.StatusNotFound
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
			return http.StatusNotFound
		}
	case "getclipboard", "getclipboardcut":
		if len(command) == 1 {
			if !getClipboard(ps, command[0] == "getclipboardcut") {
				return http.StatusInternalServerError
			}
		} else {
			return http.StatusNotFound
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
			return http.StatusNotFound
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
			return http.StatusNotFound
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
			return http.StatusNotFound
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
			return http.StatusNotFound
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
			return http.StatusNotFound
		}
	case "sleep":
		if len(command) == 2 {
			duration, err := time.ParseDuration(command[1])
			if err != nil {
				return http.StatusBadRequest
			}

			time.Sleep(duration)
		} else {
			return http.StatusNotFound
		}
	default:
		return http.StatusNotFound
	}

	return http.StatusNoContent
}
