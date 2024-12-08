package main

import (
	"encoding/binary"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func clipboardGet(cut bool, text *string, timeout time.Duration) int {
	data := make([]byte, 2)
	data[0] = 0x08
	if cut {
		data[1] = 0x02
	} else {
		data[1] = 0x01
	}

	n, err := controlSocket.Write(data)
	if err != nil {
		return http.StatusInternalServerError
	}
	if n != 2 {
		return http.StatusInternalServerError
	}

	if text != nil {
		select {
		case s := <-clipboardChannel:
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

func clipboardSet(text string, sequenceString string, paste bool, timeout time.Duration) bool {
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

	n, err := controlSocket.Write(data)
	if err != nil {
		return false
	}
	if n != len(data) {
		return false
	}

	if timeout > 0 {
		select {
		case s := <-clipboardChannel:
			if s != sequenceString {
				return false
			}
		case <-time.After(timeout):
			return false
		}
	}

	return true
}

func clipboardSendStream(w http.ResponseWriter, req *http.Request) {
	if !config.Scrcpy.Control {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var err error

	for {
		select {
		case line := <-clipboardChannel:
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
