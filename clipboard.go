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

func getClipboard(ps *portState, cut bool, text *string, timeout time.Duration) int {
	data := make([]byte, 2)
	data[0] = 0x08
	if cut {
		data[1] = 0x02
	} else {
		data[1] = 0x01
	}

	n, err := ps.controlSocket.Write(data)
	if err != nil {
		return http.StatusInternalServerError
	}
	if n != 2 {
		return http.StatusInternalServerError
	}

	if text != nil {
		select {
		case s := <-ps.clipboardChannel:
			if strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"") {
				*text = s
				return http.StatusOK
			} else {
				return http.StatusInternalServerError
			}
		case <-time.After(timeout):
			return http.StatusInternalServerError
		}
	}

	return http.StatusNoContent
}

func setClipboard(ps *portState, text string, sequenceString string, paste bool, timeout time.Duration) bool {
	var sequence uint64
	var err error

	if sequenceString != "" {
		sequence, err = strconv.ParseUint(sequenceString, 10, 64)
		if err != nil {
			return false
		}
	}

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

	if timeout > 0 {
		select {
		case s := <-ps.clipboardChannel:
			if s != sequenceString {
				return false
			}
		case <-time.After(timeout):
			return false
		}
	}

	return true
}

func sendClipboardStream(w http.ResponseWriter, req *http.Request, port int) {
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
