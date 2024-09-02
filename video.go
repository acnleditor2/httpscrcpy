package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"net/http"
	"strconv"
)

func sendVideoStream(w http.ResponseWriter, req *http.Request, port int, header bool) {
	ps, ok := portMap[port]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if !config.Ports[port].Video || config.Ports[port].VideoExtension != "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	select {
	case <-ps.videoConnectedChannel:
	case <-req.Context().Done():
		return
	}

	w.Header().Set("Device-Name", ps.deviceName)
	w.Header().Set("Codec", strconv.FormatUint(uint64(ps.videoCodec), 10))
	w.Header().Set("Initial-Width", strconv.FormatUint(uint64(ps.initialVideoWidth), 10))
	w.Header().Set("Initial-Height", strconv.FormatUint(uint64(ps.initialVideoHeight), 10))

	if header {
		headerBytes := make([]byte, 12)
		var packetSize int
		var packet []byte
		var n int
		var data []byte
		var err error

		for {
			n, err = io.ReadFull(ps.videoSocket, headerBytes)
			if err != nil {
				break
			}
			if n != 12 {
				break
			}

			packetSize = int(binary.BigEndian.Uint32(headerBytes[8:]))
			packet = make([]byte, packetSize)

			n, err = io.ReadFull(ps.videoSocket, packet)
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
		headerBytes := make([]byte, 12)
		var packetSize int
		var packet []byte
		var n int
		var err error

		for {
			n, err = io.ReadFull(ps.videoSocket, headerBytes)
			if err != nil {
				break
			}
			if n != 12 {
				break
			}

			packetSize = int(binary.BigEndian.Uint32(headerBytes[8:]))
			packet = make([]byte, packetSize)

			n, err = io.ReadFull(ps.videoSocket, packet)
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

func sendVideoToExtension(port int, ps *portState, extension *extensionState) {
	headerBytes := make([]byte, 12)
	var err error
	var packetSize int
	var packet []byte
	var n int
	var data []byte

	for {
		<-ps.videoConnectedChannel

		var b bytes.Buffer
		b.WriteByte(1)
		binary.Write(&b, binary.NativeEndian, uint16(port))
		b.WriteByte(byte(len(ps.deviceName)))
		b.WriteString(ps.deviceName)
		binary.Write(&b, binary.NativeEndian, ps.videoCodec)
		binary.Write(&b, binary.NativeEndian, ps.initialVideoWidth)
		binary.Write(&b, binary.NativeEndian, ps.initialVideoHeight)
		extension.mutex.Lock()
		_, err = b.WriteTo(extension.stdin)
		extension.mutex.Unlock()
		if err != nil {
			ps.connectionControlChannel <- false
			break
		}

		for {
			n, err = io.ReadFull(ps.videoSocket, headerBytes)
			if err != nil {
				break
			}
			if n != 12 {
				break
			}

			packetSize = int(binary.BigEndian.Uint32(headerBytes[8:]))
			packet = make([]byte, packetSize)

			n, err = io.ReadFull(ps.videoSocket, packet)
			if err != nil {
				break
			}
			if n != packetSize {
				break
			}

			data = make([]byte, 15+packetSize)
			data[0] = 2
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
