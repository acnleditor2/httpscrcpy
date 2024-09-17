package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/http"
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

func sendKeyEventSdk(ps *portState, up bool, keycode int, repeat int, metaState int) bool {
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

func sendKeyEventUhid(ps *portState, scancode int, modifiers int) bool {
	data := make([]byte, 13)
	data[0] = 0x0D
	data[2] = 0x01
	data[4] = 0x08
	data[5] = byte(modifiers)
	data[7] = byte(scancode)

	n, err := ps.controlSocket.Write(data)
	if err != nil {
		return false
	}
	if n != 13 {
		return false
	}

	return true
}

func sendTouchEvent(ps *portState, action int, x int, y int, width int, height int) bool {
	data := make([]byte, 32)
	data[0] = 0x02
	data[1] = byte(action)
	data[2] = 0xFF
	data[3] = 0xFF
	data[4] = 0xFF
	data[5] = 0xFF
	data[6] = 0xFF
	data[7] = 0xFF
	data[8] = 0xFF
	data[9] = 0xFE
	binary.BigEndian.PutUint32(data[10:], uint32(x))
	binary.BigEndian.PutUint32(data[14:], uint32(y))
	binary.BigEndian.PutUint16(data[18:], uint16(width))
	binary.BigEndian.PutUint16(data[20:], uint16(height))
	if action != 1 {
		data[22] = 0xFF
		data[23] = 0xFF
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

func sendMouseEventSdk(ps *portState, action int, x int, y int, width int, height int, button int) bool {
	data := make([]byte, 32)
	data[0] = 0x02
	data[1] = byte(action)
	data[2] = 0xFF
	data[3] = 0xFF
	data[4] = 0xFF
	data[5] = 0xFF
	data[6] = 0xFF
	data[7] = 0xFF
	data[8] = 0xFF
	data[9] = 0xFF
	binary.BigEndian.PutUint32(data[10:], uint32(x))
	binary.BigEndian.PutUint32(data[14:], uint32(y))
	binary.BigEndian.PutUint16(data[18:], uint16(width))
	binary.BigEndian.PutUint16(data[20:], uint16(height))
	if action != 1 {
		data[22] = 0xFF
		data[23] = 0xFF
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

func sendMouseEventUhid(ps *portState, x int, y int, button int) bool {
	data := make([]byte, 9)
	data[0] = 0x0D
	data[2] = 0x02
	data[4] = 0x04
	data[5] = byte(button)
	data[6] = byte(x)
	data[7] = byte(y)

	n, err := ps.controlSocket.Write(data)
	if err != nil {
		return false
	}
	if n != 9 {
		return false
	}

	return true
}

func sendScrollEventSdk(ps *portState, direction string, x int, y int, width int, height int) bool {
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

func sendScrollEventUhid(ps *portState, direction string, x int, y int) bool {
	data := make([]byte, 9)
	data[0] = 0x0D
	data[2] = 0x02
	data[4] = 0x04
	switch direction {
	case "up":
		data[8] = 0x01
	case "down":
		data[8] = 0xFF
	default:
		return true
	}

	n, err := ps.controlSocket.Write(data)
	if err != nil {
		return false
	}
	if n != 9 {
		return false
	}

	return true
}

func sendGamepadInput(ps *portState, leftX int, leftY int, rightX int, rightY int, leftTrigger int, rightTrigger int, buttons int, dpad int) bool {
	data := make([]byte, 20)
	data[0] = 0x0D
	data[2] = 0x03
	data[4] = 0x0F
	binary.LittleEndian.PutUint16(data[5:], uint16(leftX))
	binary.LittleEndian.PutUint16(data[7:], uint16(leftY))
	binary.LittleEndian.PutUint16(data[9:], uint16(rightX))
	binary.LittleEndian.PutUint16(data[11:], uint16(rightY))
	binary.LittleEndian.PutUint16(data[13:], uint16(leftTrigger))
	binary.LittleEndian.PutUint16(data[15:], uint16(rightTrigger))
	binary.LittleEndian.PutUint16(data[17:], uint16(buttons))
	data[19] = byte(dpad)

	n, err := ps.controlSocket.Write(data)
	if err != nil {
		return false
	}
	if n != 20 {
		return false
	}

	return true
}

func getMouseButton(buttonString string) int {
	switch buttonString {
	case "1", "left":
		return 1
	case "2", "right":
		return 2
	case "4", "middle":
		return 4
	default:
		return 0
	}
}

func sendUhidKeyboardOutputStream(w http.ResponseWriter, req *http.Request, port int) {
	ps, ok := portMap[port]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if !config.Ports[port].Control {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if config.Ports[port].UhidKeyboardReportDesc == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if config.Ports[port].UhidKeyboardOutputExtension != "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var err error

	for {
		select {
		case line := <-ps.uhidKeyboardOutputChannel:
			_, err = fmt.Fprintln(w, line)
			if err != nil {
				return
			}

			w.(http.Flusher).Flush()
		case <-req.Context().Done():
			return
		}
	}
}

func sendUhidKeyboardOutputToExtension(port int, ps *portState, extension *extensionState) {
	for {
		data, err := hex.DecodeString(<-ps.uhidKeyboardOutputChannel)
		if err != nil {
			ps.connectionControlChannel <- false
			break
		}

		var b bytes.Buffer
		b.WriteByte(7)
		binary.Write(&b, binary.NativeEndian, uint16(port))
		binary.Write(&b, binary.NativeEndian, uint16(len(data)))
		b.Write(data)

		extension.mutex.Lock()
		_, err = b.WriteTo(extension.stdin)
		extension.mutex.Unlock()
		if err != nil {
			ps.connectionControlChannel <- false
			break
		}
	}
}
