package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"strconv"
)

func videoStreamHandler(w http.ResponseWriter, req *http.Request) {
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
		if port == 0 {
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

		if !config.Ports[port].Video || config.Ports[port].VideoExtension != "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		stripHeader := true
		var err error

		if query.Has("stripheader") {
			stripHeader, err = strconv.ParseBool(query.Get("stripheader"))
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}

		network := query.Get("network")
		address := query.Get("address")
		var sendSocket net.Conn

		if address != "" {
			if network == "" {
				network = "tcp"
			}

			sendSocket, err = net.Dial(network, address)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}

		if address == "" {
			select {
			case <-ps.videoConnectedChannel:
			case <-req.Context().Done():
				return
			}

			w.Header().Set("Device-Name", ps.deviceName)
			w.Header().Set("Codec", strconv.FormatUint(uint64(ps.videoCodec), 10))
			w.Header().Set("Initial-Width", strconv.FormatUint(uint64(ps.initialVideoWidth), 10))
			w.Header().Set("Initial-Height", strconv.FormatUint(uint64(ps.initialVideoHeight), 10))

			if stripHeader {
				headerBytes := make([]byte, 12)
				var packetSize int
				var packet []byte
				var n int

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
			} else {
				headerBytes := make([]byte, 12)
				var packetSize int
				var packet []byte
				var n int
				var data []byte

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
			}
		} else {
			w.WriteHeader(http.StatusNoContent)

			select {
			case <-ps.videoConnectedChannel:
			case <-req.Context().Done():
				return
			}

			go func() {
				if stripHeader {
					headerBytes := make([]byte, 12)
					var packetSize int
					var packet []byte
					var n int
					var err error

					for {
						n, err = io.ReadFull(ps.videoSocket, headerBytes)
						if err != nil {
							sendSocket.Close()
							break
						}
						if n != 12 {
							sendSocket.Close()
							break
						}

						packetSize = int(binary.BigEndian.Uint32(headerBytes[8:]))
						packet = make([]byte, packetSize)

						n, err = io.ReadFull(ps.videoSocket, packet)
						if err != nil {
							sendSocket.Close()
							break
						}
						if n != packetSize {
							sendSocket.Close()
							break
						}

						n, err = sendSocket.Write(packet)
						if err != nil {
							ps.connectionControlChannel <- false
							sendSocket.Close()
							break
						}
						if n != packetSize {
							ps.connectionControlChannel <- false
							sendSocket.Close()
							break
						}
					}
				} else {
					io.Copy(sendSocket, ps.videoSocket)
					sendSocket.Close()
				}
			}()
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
