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

func runCommands(ps *portState, port int, commands [][]string) {
	for _, command := range commands {
		if ps.controlSocket == nil && command[0] != "connect" && command[0] != "startscrcpyserver" && command[0] != "sleep" && command[0] != "adb" {
			return
		}

		switch command[0] {
		case "connect":
			if len(command) == 1 {
				select {
				case ps.connectionControlChannel <- true:
				default:
					return
				}
			} else {
				return
			}
		case "disconnect":
			if len(command) == 1 {
				if ps.scrcpyServer != nil {
					return
				}

				select {
				case ps.connectionControlChannel <- false:
				default:
					return
				}
			} else {
				return
			}
		case "startscrcpyserver":
			if len(command) == 1 {
				if len(config.Ports[port].ADB) == 0 {
					return
				}

				if len(config.Ports[port].ScrcpyServer) != 2 {
					return
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
				ps.scrcpyServer.Stdout = os.Stdout
				ps.scrcpyServer.Stderr = os.Stderr

				if ps.scrcpyServer.Start() != nil {
					ps.scrcpyServer = nil
					return
				}
			} else {
				return
			}
		case "stopscrcpyserver":
			if len(command) == 1 {
				if ps.scrcpyServer == nil {
					return
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
				return
			}
		case "key", "key2":
			if len(command) == 2 || len(command) == 5 {
				var keycode int
				var err error

				if command[0] == "key" {
					keycode = keycodeMap[command[1]]
					if keycode == 0 {
						return
					}
				} else {
					keycode, err = strconv.Atoi(command[1])
					if err != nil {
						return
					}
				}

				if len(command) == 2 {
					if !sendKeyEventSdk(ps, false, keycode, 0, 0) {
						return
					}

					if !sendKeyEventSdk(ps, true, keycode, 0, 0) {
						return
					}
				} else {
					up, err := strconv.ParseBool(command[2])
					if err != nil {
						return
					}

					repeat, err := strconv.Atoi(command[3])
					if err != nil {
						return
					}

					metaState, err := strconv.Atoi(command[4])
					if err != nil {
						return
					}

					if !sendKeyEventSdk(ps, up, keycode, repeat, metaState) {
						return
					}
				}
			} else {
				return
			}
		case "key3":
			if (len(command) == 2 || len(command) == 3) && config.Ports[port].UhidKeyboardReportDesc != "" {
				scancode, err := strconv.Atoi(command[1])
				if err != nil {
					return
				}

				if len(command) == 2 {
					if !sendKeyEventUhid(ps, scancode, 0) {
						return
					}

					if scancode != 0 {
						if !sendKeyEventUhid(ps, 0, 0) {
							return
						}
					}
				} else {
					modifiers, err := strconv.Atoi(command[2])
					if err != nil {
						return
					}

					if !sendKeyEventUhid(ps, scancode, modifiers) {
						return
					}
				}
			} else {
				return
			}
		case "type", "typebase64", "typebase64url", "typehex":
			if len(command) == 2 {
				if command[1] == "" {
					return
				}

				var text string

				if command[0] == "typebase64" {
					textBytes, err := base64.StdEncoding.DecodeString(command[1])
					if err != nil {
						return
					}
					text = string(textBytes)
				} else if command[0] == "typebase64url" {
					textBytes, err := base64.URLEncoding.DecodeString(command[1])
					if err != nil {
						return
					}
					text = string(textBytes)
				} else if command[0] == "typehex" {
					textBytes, err := hex.DecodeString(command[1])
					if err != nil {
						return
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
					return
				}
				if n != len(data) {
					return
				}
			} else {
				return
			}
		case "touch":
			if len(command) == 5 {
				x, err := strconv.Atoi(command[1])
				if err != nil {
					return
				}

				y, err := strconv.Atoi(command[2])
				if err != nil {
					return
				}

				width, err := strconv.Atoi(command[3])
				if err != nil {
					return
				}

				height, err := strconv.Atoi(command[4])
				if err != nil {
					return
				}

				if !sendTouchEvent(ps, 0, x, y, width, height) {
					return
				}

				if !sendTouchEvent(ps, 1, x, y, width, height) {
					return
				}
			} else {
				return
			}
		case "touchdown":
			if len(command) == 5 {
				x, err := strconv.Atoi(command[1])
				if err != nil {
					return
				}

				y, err := strconv.Atoi(command[2])
				if err != nil {
					return
				}

				width, err := strconv.Atoi(command[3])
				if err != nil {
					return
				}

				height, err := strconv.Atoi(command[4])
				if err != nil {
					return
				}

				if !sendTouchEvent(ps, 0, x, y, width, height) {
					return
				}
			} else {
				return
			}
		case "touchup":
			if len(command) == 5 {
				x, err := strconv.Atoi(command[1])
				if err != nil {
					return
				}

				y, err := strconv.Atoi(command[2])
				if err != nil {
					return
				}

				width, err := strconv.Atoi(command[3])
				if err != nil {
					return
				}

				height, err := strconv.Atoi(command[4])
				if err != nil {
					return
				}

				if !sendTouchEvent(ps, 1, x, y, width, height) {
					return
				}
			} else {
				return
			}
		case "touchmove":
			if len(command) == 5 {
				x, err := strconv.Atoi(command[1])
				if err != nil {
					return
				}

				y, err := strconv.Atoi(command[2])
				if err != nil {
					return
				}

				width, err := strconv.Atoi(command[3])
				if err != nil {
					return
				}

				height, err := strconv.Atoi(command[4])
				if err != nil {
					return
				}

				if !sendTouchEvent(ps, 2, x, y, width, height) {
					return
				}
			} else {
				return
			}
		case "mouseclick":
			if len(command) == 4 && config.Ports[port].UhidMouseReportDesc != "" {
				x, err := strconv.Atoi(command[2])
				if err != nil {
					return
				}

				y, err := strconv.Atoi(command[3])
				if err != nil {
					return
				}

				button := getMouseButton(command[1])

				if !sendMouseEventUhid(ps, x, y, button) {
					return
				}

				if !sendMouseEventUhid(ps, 0, 0, 0) {
					return
				}
			} else if len(command) == 6 {
				x, err := strconv.Atoi(command[2])
				if err != nil {
					return
				}

				y, err := strconv.Atoi(command[3])
				if err != nil {
					return
				}

				width, err := strconv.Atoi(command[4])
				if err != nil {
					return
				}

				height, err := strconv.Atoi(command[5])
				if err != nil {
					return
				}

				button := getMouseButton(command[1])

				if !sendMouseEventSdk(ps, 0, x, y, width, height, button) {
					return
				}

				if !sendMouseEventSdk(ps, 1, x, y, width, height, button) {
					return
				}
			} else {
				return
			}
		case "mousedown":
			if len(command) == 4 && config.Ports[port].UhidMouseReportDesc != "" {
				x, err := strconv.Atoi(command[2])
				if err != nil {
					return
				}

				y, err := strconv.Atoi(command[3])
				if err != nil {
					return
				}

				if !sendMouseEventUhid(ps, x, y, getMouseButton(command[1])) {
					return
				}
			} else if len(command) == 6 {
				x, err := strconv.Atoi(command[2])
				if err != nil {
					return
				}

				y, err := strconv.Atoi(command[3])
				if err != nil {
					return
				}

				width, err := strconv.Atoi(command[4])
				if err != nil {
					return
				}

				height, err := strconv.Atoi(command[5])
				if err != nil {
					return
				}

				if !sendMouseEventSdk(ps, 0, x, y, width, height, getMouseButton(command[1])) {
					return
				}
			} else {
				return
			}
		case "mouseup":
			if len(command) == 1 && config.Ports[port].UhidMouseReportDesc != "" {
				if !sendMouseEventUhid(ps, 0, 0, 0) {
					return
				}
			} else if len(command) == 6 && config.Ports[port].UhidMouseReportDesc != "" {
				x, err := strconv.Atoi(command[2])
				if err != nil {
					return
				}

				y, err := strconv.Atoi(command[3])
				if err != nil {
					return
				}

				width, err := strconv.Atoi(command[4])
				if err != nil {
					return
				}

				height, err := strconv.Atoi(command[5])
				if err != nil {
					return
				}

				if !sendMouseEventSdk(ps, 1, x, y, width, height, getMouseButton(command[1])) {
					return
				}
			} else {
				return
			}
		case "mousemove":
			if len(command) == 3 && config.Ports[port].UhidMouseReportDesc != "" {
				x, err := strconv.Atoi(command[1])
				if err != nil {
					return
				}

				y, err := strconv.Atoi(command[2])
				if err != nil {
					return
				}

				if !sendMouseEventUhid(ps, x, y, 0) {
					return
				}
			} else if len(command) == 4 && config.Ports[port].UhidMouseReportDesc != "" {
				x, err := strconv.Atoi(command[2])
				if err != nil {
					return
				}

				y, err := strconv.Atoi(command[3])
				if err != nil {
					return
				}

				if !sendMouseEventUhid(ps, x, y, getMouseButton(command[1])) {
					return
				}
			} else if len(command) == 6 {
				x, err := strconv.Atoi(command[2])
				if err != nil {
					return
				}

				y, err := strconv.Atoi(command[3])
				if err != nil {
					return
				}

				width, err := strconv.Atoi(command[4])
				if err != nil {
					return
				}

				height, err := strconv.Atoi(command[5])
				if err != nil {
					return
				}

				if !sendMouseEventSdk(ps, 2, x, y, width, height, getMouseButton(command[1])) {
					return
				}
			} else {
				return
			}
		case "scrollleft", "scrollright", "scrollup", "scrolldown":
			if len(command) == 3 && config.Ports[port].UhidMouseReportDesc != "" && (command[0] == "scrollup" || command[0] == "scrolldown") {
				x, err := strconv.Atoi(command[1])
				if err != nil {
					return
				}

				y, err := strconv.Atoi(command[2])
				if err != nil {
					return
				}

				if !sendScrollEventUhid(ps, command[0][6:], x, y) {
					return
				}
			} else if len(command) == 5 {
				x, err := strconv.Atoi(command[1])
				if err != nil {
					return
				}

				y, err := strconv.Atoi(command[2])
				if err != nil {
					return
				}

				width, err := strconv.Atoi(command[3])
				if err != nil {
					return
				}

				height, err := strconv.Atoi(command[4])
				if err != nil {
					return
				}

				if !sendScrollEventSdk(ps, command[0][6:], x, y, width, height) {
					return
				}
			} else {
				return
			}
		case "gamepadinput":
			if len(command) == 9 {
				leftX, err := strconv.Atoi(command[1])
				if err != nil {
					return
				}

				leftY, err := strconv.Atoi(command[2])
				if err != nil {
					return
				}

				rightX, err := strconv.Atoi(command[3])
				if err != nil {
					return
				}

				rightY, err := strconv.Atoi(command[4])
				if err != nil {
					return
				}

				leftTrigger, err := strconv.Atoi(command[5])
				if err != nil {
					return
				}

				rightTrigger, err := strconv.Atoi(command[6])
				if err != nil {
					return
				}

				buttons, err := strconv.Atoi(command[7])
				if err != nil {
					return
				}

				dpad, err := strconv.Atoi(command[8])
				if err != nil {
					return
				}

				if !sendGamepadInput(ps, leftX, leftY, rightX, rightY, leftTrigger, rightTrigger, buttons, dpad) {
					return
				}
			} else {
				return
			}
		case "openhardkeyboardsettings":
			if len(command) == 1 {
				n, err := ps.controlSocket.Write([]byte{0x0F})
				if err != nil {
					return
				}
				if n != 1 {
					return
				}
			} else {
				return
			}
		case "backorscreenon":
			if len(command) == 1 {
				n, err := ps.controlSocket.Write([]byte{0x04, 0x00, 0x04, 0x01})
				if err != nil {
					return
				}
				if n != 4 {
					return
				}
			} else {
				return
			}
		case "expandnotificationspanel":
			if len(command) == 1 {
				n, err := ps.controlSocket.Write([]byte{0x05})
				if err != nil {
					return
				}
				if n != 1 {
					return
				}
			} else {
				return
			}
		case "expandsettingspanel":
			if len(command) == 1 {
				n, err := ps.controlSocket.Write([]byte{0x06})
				if err != nil {
					return
				}
				if n != 1 {
					return
				}
			} else {
				return
			}
		case "collapsepanels":
			if len(command) == 1 {
				n, err := ps.controlSocket.Write([]byte{0x07})
				if err != nil {
					return
				}
				if n != 1 {
					return
				}
			} else {
				return
			}
		case "getclipboard", "getclipboardcut":
			if len(command) == 1 {
				if getClipboard(ps, command[0] == "getclipboardcut", nil, 0) != http.StatusNoContent {
					return
				}
			} else {
				return
			}
		case "setclipboard", "setclipboardbase64", "setclipboardbase64url", "setclipboardhex", "setclipboardpaste", "setclipboardpastebase64", "setclipboardpastebase64url", "setclipboardpastehex":
			if len(command) == 2 || len(command) == 3 || len(command) == 4 {
				var text string

				if strings.HasSuffix(command[0], "base64") {
					decoded, err := base64.StdEncoding.DecodeString(command[1])
					if err != nil {
						return
					}
					text = string(decoded)
				} else if strings.HasSuffix(command[0], "base64url") {
					decoded, err := base64.URLEncoding.DecodeString(command[1])
					if err != nil {
						return
					}
					text = string(decoded)
				} else if strings.HasSuffix(command[0], "hex") {
					decoded, err := hex.DecodeString(command[1])
					if err != nil {
						return
					}
					text = string(decoded)
				} else {
					text = command[1]
				}

				var sequenceString string
				var timeout time.Duration
				var err error

				if len(command) > 2 {
					sequenceString = command[2]

					if len(command) == 4 {
						timeout, err = time.ParseDuration(command[3])
						if err != nil {
							return
						}
					}
				}

				if !setClipboard(ps, text, sequenceString, strings.HasPrefix(command[0], "setclipboardpaste"), timeout) {
					return
				}
			} else {
				return
			}
		case "turnscreenon":
			if len(command) == 1 {
				n, err := ps.controlSocket.Write([]byte{0x0A, 0x02})
				if err != nil {
					return
				}
				if n != 2 {
					return
				}
			} else {
				return
			}
		case "turnscreenoff":
			if len(command) == 1 {
				n, err := ps.controlSocket.Write([]byte{0x0A, 0x00})
				if err != nil {
					return
				}
				if n != 2 {
					return
				}
			} else {
				return
			}
		case "rotate":
			if len(command) == 1 {
				n, err := ps.controlSocket.Write([]byte{0x0B})
				if err != nil {
					return
				}
				if n != 1 {
					return
				}
			} else {
				return
			}
		case "senddata":
			if len(command) == 2 {
				data, err := hex.DecodeString(command[1])
				if err != nil {
					return
				}
				if len(data) == 0 {
					return
				}

				n, err := ps.controlSocket.Write(data)
				if err != nil {
					return
				}
				if n != len(data) {
					return
				}
			} else {
				return
			}
		case "sleep":
			if len(command) == 2 {
				duration, err := time.ParseDuration(command[1])
				if err != nil {
					return
				}

				time.Sleep(duration)
			} else {
				return
			}
		case "adb":
			if len(command) > 1 {
				cmd := exec.Command(config.Ports[port].ADB[0], command[1:]...)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr

				if cmd.Run() != nil {
					return
				}
			} else {
				return
			}
		}
	}
}
