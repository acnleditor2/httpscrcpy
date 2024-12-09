package main

import (
	"encoding/binary"
	"io"
	"net/http"
	"strconv"
)

func audioSendStream(w http.ResponseWriter, req *http.Request, header bool) {
	if !config.Scrcpy.Audio {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	select {
	case <-audioConnectedChannel:
	case <-req.Context().Done():
		return
	}

	if req.Header.Get("Origin") != "" {
		w.Header().Set("Access-Control-Expose-Headers", "Device-Name, Codec")
	}

	w.Header().Set("Device-Name", deviceName)
	w.Header().Set("Codec", strconv.FormatUint(uint64(audioCodec), 10))

	headerBytes := make([]byte, 12)
	var packetSize int
	var packet []byte
	var n int
	var err error

	if header {
		var data []byte

		for {
			n, err = io.ReadFull(audioSocket, headerBytes)
			if err != nil {
				break
			}
			if n != 12 {
				break
			}

			packetSize = int(binary.BigEndian.Uint32(headerBytes[8:]))
			packet = make([]byte, packetSize)

			n, err = io.ReadFull(audioSocket, packet)
			if err != nil {
				break
			}
			if n != packetSize {
				break
			}

			data = make([]byte, 12+packetSize)
			copy(data[:12], headerBytes)
			copy(data[12:12+packetSize], packet)

			n, err = w.Write(data)
			if err != nil {
				connectionControlChannel <- false
				break
			}
			if n < 12+packetSize {
				connectionControlChannel <- false
				break
			}

			w.(http.Flusher).Flush()
		}
	} else {
		for {
			n, err = io.ReadFull(audioSocket, headerBytes)
			if err != nil {
				break
			}
			if n != 12 {
				break
			}

			packetSize = int(binary.BigEndian.Uint32(headerBytes[8:]))
			packet = make([]byte, packetSize)

			n, err = io.ReadFull(audioSocket, packet)
			if err != nil {
				break
			}
			if n != packetSize {
				break
			}

			n, err = w.Write(packet)
			if err != nil {
				connectionControlChannel <- false
				break
			}
			if n < packetSize {
				connectionControlChannel <- false
				break
			}

			w.(http.Flusher).Flush()
		}
	}
}
