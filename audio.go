package main

import (
	"encoding/binary"
	"io"
	"net/http"
	"strconv"
)

func sendAudioStream(w http.ResponseWriter, req *http.Request, port int, header bool) {
	ps, ok := portMap[port]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if !config.Ports[port].Audio || config.Ports[port].AudioExtension != "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	select {
	case <-ps.audioConnectedChannel:
	case <-req.Context().Done():
		return
	}

	if req.Header.Get("Origin") != "" {
		w.Header().Set("Access-Control-Expose-Headers", "Device-Name, Codec")
	}

	w.Header().Set("Device-Name", ps.deviceName)
	w.Header().Set("Codec", strconv.FormatUint(uint64(ps.audioCodec), 10))

	headerBytes := make([]byte, 12)
	var packetSize int
	var packet []byte
	var n int
	var err error

	if header {
		var data []byte

		for {
			n, err = io.ReadFull(ps.audioSocket, headerBytes)
			if err != nil {
				break
			}
			if n != 12 {
				break
			}

			packetSize = int(binary.BigEndian.Uint32(headerBytes[8:]))
			packet = make([]byte, packetSize)

			n, err = io.ReadFull(ps.audioSocket, packet)
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
				ps.connectionControlChannel <- false
				break
			}
			if n < 12+packetSize {
				ps.connectionControlChannel <- false
				break
			}

			w.(http.Flusher).Flush()
		}
	} else {
		for {
			n, err = io.ReadFull(ps.audioSocket, headerBytes)
			if err != nil {
				break
			}
			if n != 12 {
				break
			}

			packetSize = int(binary.BigEndian.Uint32(headerBytes[8:]))
			packet = make([]byte, packetSize)

			n, err = io.ReadFull(ps.audioSocket, packet)
			if err != nil {
				break
			}
			if n != packetSize {
				break
			}

			n, err = w.Write(packet)
			if err != nil {
				ps.connectionControlChannel <- false
				break
			}
			if n < packetSize {
				ps.connectionControlChannel <- false
				break
			}

			w.(http.Flusher).Flush()
		}
	}
}

func sendAudioToExtension(port int, ps *portState, extension *extensionState) {
	headerBytes := make([]byte, 12)
	var err error
	var packetSize int
	var packet []byte
	var n int
	var data []byte

	for {
		<-ps.audioConnectedChannel

		data = make([]byte, 7)
		data[0] = 3
		binary.NativeEndian.PutUint16(data[1:3], uint16(port))
		binary.NativeEndian.PutUint32(data[3:], ps.audioCodec)
		extension.mutex.Lock()
		n, err = extension.stdin.Write(data)
		extension.mutex.Unlock()
		if err != nil {
			ps.connectionControlChannel <- false
			break
		}
		if n < 7 {
			ps.connectionControlChannel <- false
			break
		}

		for {
			n, err = io.ReadFull(ps.audioSocket, headerBytes)
			if err != nil {
				break
			}
			if n != 12 {
				break
			}

			packetSize = int(binary.BigEndian.Uint32(headerBytes[8:]))
			packet = make([]byte, packetSize)

			n, err = io.ReadFull(ps.audioSocket, packet)
			if err != nil {
				break
			}
			if n != packetSize {
				break
			}

			data = make([]byte, 15+packetSize)
			data[0] = 4
			binary.NativeEndian.PutUint16(data[1:3], uint16(port))
			copy(data[3:15], headerBytes)
			copy(data[15:15+packetSize], packet)

			extension.mutex.Lock()
			n, err = extension.stdin.Write(data)
			extension.mutex.Unlock()
			if err != nil {
				ps.connectionControlChannel <- false
				break
			}
			if n < 15+packetSize {
				ps.connectionControlChannel <- false
				break
			}
		}
	}
}
