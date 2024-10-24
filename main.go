package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type portState struct {
	listener                  net.Listener
	videoSocket               net.Conn
	audioSocket               net.Conn
	controlSocket             net.Conn
	connectionControlChannel  chan bool
	videoConnectedChannel     chan struct{}
	audioConnectedChannel     chan struct{}
	clipboardChannel          chan string
	uhidKeyboardOutputChannel chan string
	deviceName                string
	videoCodec                uint32
	audioCodec                uint32
	initialVideoWidth         int
	initialVideoHeight        int
	scrcpyServer              *exec.Cmd
	connectedCommands         [][]string
	videoFrame                []byte
	videoFrameMutex           sync.RWMutex
}

type Port struct {
	Adb                         []string   `json:"adb"`
	Ffmpeg                      string     `json:"ffmpeg"`
	Video                       bool       `json:"video"`
	VideoStream                 bool       `json:"videoStream"`
	VideoFrameAlpha             bool       `json:"videoFrameAlpha"`
	Audio                       bool       `json:"audio"`
	Control                     bool       `json:"control"`
	Forward                     bool       `json:"forward"`
	UhidKeyboardReportDesc      string     `json:"uhidKeyboardReportDesc"`
	UhidMouseReportDesc         string     `json:"uhidMouseReportDesc"`
	UhidGamepadReportDesc       string     `json:"uhidGamepadReportDesc"`
	VideoExtension              string     `json:"videoExtension"`
	AudioExtension              string     `json:"audioExtension"`
	ClipboardStreamExtension    string     `json:"clipboardStreamExtension"`
	UhidKeyboardOutputExtension string     `json:"uhidKeyboardOutputExtension"`
	Device                      string     `json:"device"`
	ScrcpyServer                []string   `json:"scrcpyServer"`
	ScrcpyServerOptions         []string   `json:"scrcpyServerOptions"`
	ClipboardAutosync           bool       `json:"clipboardAutosync"`
	Cleanup                     bool       `json:"cleanup"`
	PowerOn                     bool       `json:"powerOn"`
	ConnectedCommands           [][]string `json:"connectedCommands"`
}

type Endpoint struct {
	Port             int        `json:"port"`
	Commands         [][]string `json:"commands"`
	Response         string     `json:"response"`
	ClipboardCut     bool       `json:"clipboardCut"`
	ClipboardTimeout int        `json:"clipboardTimeout"`
}

type Config struct {
	Adb        []string            `json:"adb"`
	Ffmpeg     string              `json:"ffmpeg"`
	Address    string              `json:"address"`
	Static     string              `json:"static"`
	Cert       string              `json:"cert"`
	Key        string              `json:"key"`
	Ports      map[int]Port        `json:"ports"`
	Endpoints  map[string]Endpoint `json:"endpoints"`
	Extensions [][]string          `json:"extensions"`
}

var portMap = map[int]*portState{}
var config Config

func list(port int, serverArg string) (string, int) {
	if len(config.Adb) == 0 && len(config.Ports[port].Adb) == 0 {
		return "", http.StatusNotFound
	}

	if len(config.Ports[port].ScrcpyServer) != 2 {
		return "", http.StatusNotFound
	}

	var adb []string
	var args []string

	if len(config.Ports[port].Adb) > 0 {
		adb = config.Ports[port].Adb
	} else {
		adb = config.Adb
	}

	if config.Ports[port].Device == "usb" {
		args = append(adb[1:], "-d")
	} else if config.Ports[port].Device == "tcpip" {
		args = append(adb[1:], "-e")
	} else if config.Ports[port].Device != "" {
		args = append(adb[1:], "-s", config.Ports[port].Device)
	} else {
		args = adb[1:]
	}

	args = append(
		args,
		"shell",
		fmt.Sprintf("CLASSPATH=%s", config.Ports[port].ScrcpyServer[0]),
		"app_process",
		"/",
		"com.genymobile.scrcpy.Server",
		config.Ports[port].ScrcpyServer[1],
		serverArg,
	)

	if !config.Ports[port].Cleanup {
		args = append(args, "cleanup=false")
	}

	output, err := exec.Command(adb[0], args...).CombinedOutput()
	if err != nil {
		return string(output), http.StatusInternalServerError
	}

	return string(output), http.StatusOK
}

func readDummyByte(c net.Conn) bool {
	data := make([]byte, 1)

	n, err := c.Read(data)
	if err != nil {
		return false
	}
	if n != 1 {
		return false
	}

	return true
}

func readDeviceMeta(port int) bool {
	ps := portMap[port]
	data := make([]byte, 64)
	var n int
	var err error

	if config.Ports[port].Video {
		n, err = io.ReadFull(ps.videoSocket, data)
	} else if config.Ports[port].Audio {
		n, err = io.ReadFull(ps.audioSocket, data)
	} else {
		n, err = io.ReadFull(ps.controlSocket, data)
	}

	if err != nil {
		return false
	}

	if n != 64 {
		return false
	}

	ps.deviceName = string(data[:bytes.IndexByte(data, 0)])

	return true
}

func endpointHandler(w http.ResponseWriter, req *http.Request) {
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

		endpoint := config.Endpoints[req.URL.Path]
		query := req.URL.Query()
		var port int
		var err error

		if len(config.Ports) == 1 {
			for p := range config.Ports {
				port = p
			}
		} else if endpoint.Port == 0 {
			port, err = strconv.Atoi(query.Get("port"))
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		} else {
			port = endpoint.Port
		}

		ps, ok := portMap[port]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if len(endpoint.Commands) > 0 {
			commands := make([][]string, len(endpoint.Commands))
			for i := range endpoint.Commands {
				commands[i] = make([]string, len(endpoint.Commands[i]))
				for j := range endpoint.Commands[i] {
					commands[i][j] = os.Expand(endpoint.Commands[i][j], query.Get)
				}
			}

			go runCommands(ps, port, commands)
			w.WriteHeader(http.StatusNoContent)
		} else {
			switch endpoint.Response {
			case "videoStream":
				sendVideoStream(w, req, port, true)
			case "rawVideoStream":
				sendVideoStream(w, req, port, false)
			case "audioStream":
				sendAudioStream(w, req, port, true)
			case "rawAudioStream":
				sendAudioStream(w, req, port, false)
			case "clipboardStream":
				sendClipboardStream(w, req, port)
			case "uhidKeyboardOutputStream":
				sendUhidKeyboardOutputStream(w, req, port)
			case "clipboard":
				if ps.controlSocket == nil {
					w.WriteHeader(http.StatusNotFound)
					return
				}

				var text string
				status := getClipboard(ps, endpoint.ClipboardCut, &text, time.Duration(endpoint.ClipboardTimeout)*time.Millisecond)

				if status != http.StatusOK {
					w.WriteHeader(status)
					return
				}

				w.Write([]byte(text))
			case "deviceName":
				if ps.deviceName == "" {
					w.WriteHeader(http.StatusNotFound)
				} else {
					w.Write([]byte(ps.deviceName))
				}
			case "videoCodec":
				if ps.videoCodec == 0 {
					w.WriteHeader(http.StatusNotFound)
				} else {
					w.Write([]byte(strconv.FormatUint(uint64(ps.videoCodec), 10)))
				}
			case "audioCodec":
				if ps.audioCodec == 0 {
					w.WriteHeader(http.StatusNotFound)
				} else {
					w.Write([]byte(strconv.FormatUint(uint64(ps.audioCodec), 10)))
				}
			case "initialVideoWidth":
				if ps.initialVideoWidth == 0 {
					w.WriteHeader(http.StatusNotFound)
				} else {
					w.Write([]byte(strconv.Itoa(ps.initialVideoWidth)))
				}
			case "initialVideoHeight":
				if ps.initialVideoHeight == 0 {
					w.WriteHeader(http.StatusNotFound)
				} else {
					w.Write([]byte(strconv.Itoa(ps.initialVideoHeight)))
				}
			case "videoFrame":
				sendVideoFrame(w, req, port)
			case "encoders", "displays", "cameras", "apps":
				output, status := list(port, fmt.Sprintf("list_%s=true", endpoint.Response))

				if status != http.StatusOK {
					w.WriteHeader(status)
				}

				if output != "" {
					w.Write([]byte(output))
				}
			case "cameraSizes":
				output, status := list(port, "list_camera_sizes=true")

				if status != http.StatusOK {
					w.WriteHeader(status)
				}

				if output != "" {
					w.Write([]byte(output))
				}
			}
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

func main() {
	if len(os.Args) != 1 && len(os.Args) != 2 {
		os.Exit(1)
	}

	var configBytes []byte
	var err error

	if len(os.Args) == 1 || os.Args[1] == "-" {
		configBytes, err = io.ReadAll(os.Stdin)
	} else if strings.HasPrefix(os.Args[1], "http://") || strings.HasPrefix(os.Args[1], "https://") {
		response, err := http.Get(os.Args[1])
		if err != nil {
			panic(err)
		}

		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			os.Exit(1)
		}

		configBytes, err = io.ReadAll(response.Body)
		response.Body.Close()
	} else {
		configBytes, err = os.ReadFile(os.Args[1])
	}

	if err != nil {
		panic(err)
	}

	err = json.Unmarshal(configBytes, &config)
	if err != nil {
		panic(err)
	}

	if config.Address == "" || len(config.Ports) == 0 {
		os.Exit(1)
	}

	for port := range config.Ports {
		portMap[port] = &portState{
			connectionControlChannel:  make(chan bool),
			videoConnectedChannel:     make(chan struct{}),
			audioConnectedChannel:     make(chan struct{}),
			clipboardChannel:          make(chan string),
			uhidKeyboardOutputChannel: make(chan string),
			connectedCommands:         config.Ports[port].ConnectedCommands,
		}

		if config.Ports[port].Video && !config.Ports[port].VideoStream && (config.Ports[port].Ffmpeg != "" || config.Ffmpeg != "") && config.Ports[port].VideoExtension == "" {
			go sendVideoToFfmpeg(port, portMap[port])
		}

		go func(p int) {
			ps := portMap[p]
			var err error

			if !config.Ports[p].Forward {
				ps.listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
				if err != nil {
					return
				}
			}

			for connect := range ps.connectionControlChannel {
				if connect {
					if config.Ports[p].Forward {
						var connected bool

						for i := 0; i < 100; i++ {
							if ps.videoSocket != nil {
								ps.videoSocket.Close()
							}

							if ps.audioSocket != nil {
								ps.audioSocket.Close()
							}

							if ps.controlSocket != nil {
								ps.controlSocket.Close()
							}

							if i != 0 {
								time.Sleep(100 * time.Millisecond)
							}

							if config.Ports[p].Video {
								ps.videoSocket, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", p))
								if err != nil {
									return
								}

								if !readDummyByte(ps.videoSocket) {
									continue
								}
							}

							if config.Ports[p].Audio {
								ps.audioSocket, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", p))
								if err != nil {
									return
								}

								if !config.Ports[p].Video && !readDummyByte(ps.audioSocket) {
									continue
								}
							}

							if config.Ports[p].Control {
								ps.controlSocket, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", p))
								if err != nil {
									return
								}

								if !config.Ports[p].Video && !config.Ports[p].Audio && !readDummyByte(ps.controlSocket) {
									continue
								}
							}

							connected = true
							break
						}

						if !connected {
							continue
						}
					} else {
						if ps.videoSocket != nil {
							ps.videoSocket.Close()
						}

						if ps.audioSocket != nil {
							ps.audioSocket.Close()
						}

						if ps.controlSocket != nil {
							ps.controlSocket.Close()
						}

						if config.Ports[p].Video {
							ps.videoSocket, err = ps.listener.Accept()
							if err != nil {
								return
							}
						}

						if config.Ports[p].Audio {
							ps.audioSocket, err = ps.listener.Accept()
							if err != nil {
								return
							}
						}

						if config.Ports[p].Control {
							ps.controlSocket, err = ps.listener.Accept()
							if err != nil {
								return
							}
						}
					}

					if !readDeviceMeta(p) {
						continue
					}

					if config.Ports[p].Video {
						data := make([]byte, 12)
						n, err := io.ReadFull(ps.videoSocket, data)
						if err != nil {
							continue
						}
						if n != 12 {
							continue
						}

						ps.videoCodec = binary.BigEndian.Uint32(data[:4])
						ps.initialVideoWidth = int(binary.BigEndian.Uint32(data[4:8]))
						ps.initialVideoHeight = int(binary.BigEndian.Uint32(data[8:]))
					}

					if config.Ports[p].Audio {
						data := make([]byte, 4)
						n, err := io.ReadFull(ps.audioSocket, data)
						if err != nil {
							continue
						}
						if n != 4 {
							continue
						}

						ps.audioCodec = binary.BigEndian.Uint32(data)
					}

					if config.Ports[p].Control {
						if config.Ports[p].UhidKeyboardReportDesc != "" {
							reportDesc, err := hex.DecodeString(config.Ports[p].UhidKeyboardReportDesc)
							if err != nil {
								return
							}

							data := make([]byte, 6+len(reportDesc))
							data[0] = 0x0C
							data[2] = 0x01
							binary.BigEndian.PutUint16(data[4:6], uint16(len(reportDesc)))
							copy(data[6:], reportDesc)

							n, err := ps.controlSocket.Write(data)
							if err != nil {
								return
							}
							if n != len(data) {
								return
							}
						}

						if config.Ports[p].UhidMouseReportDesc != "" {
							reportDesc, err := hex.DecodeString(config.Ports[p].UhidMouseReportDesc)
							if err != nil {
								return
							}

							data := make([]byte, 6+len(reportDesc))
							data[0] = 0x0C
							data[2] = 0x02
							binary.BigEndian.PutUint16(data[4:6], uint16(len(reportDesc)))
							copy(data[6:], reportDesc)

							n, err := ps.controlSocket.Write(data)
							if err != nil {
								return
							}
							if n != len(data) {
								return
							}
						}

						if config.Ports[p].UhidGamepadReportDesc != "" {
							reportDesc, err := hex.DecodeString(config.Ports[p].UhidGamepadReportDesc)
							if err != nil {
								return
							}

							data := make([]byte, 6+len(reportDesc))
							data[0] = 0x0C
							data[2] = 0x03
							binary.BigEndian.PutUint16(data[4:6], uint16(len(reportDesc)))
							copy(data[6:], reportDesc)

							n, err := ps.controlSocket.Write(data)
							if err != nil {
								return
							}
							if n != len(data) {
								return
							}
						}

						go func() {
							data := make([]byte, 262144)

							for {
								n, err := io.ReadFull(ps.controlSocket, data[:1])
								if err != nil {
									return
								}
								if n != 1 {
									return
								}

								switch data[0] {
								case 0:
									n, err = io.ReadFull(ps.controlSocket, data[1:5])
									if err != nil {
										return
									}
									if n != 4 {
										return
									}

									clipboardLength := int(binary.BigEndian.Uint32(data[1:5]))

									n, err = io.ReadFull(ps.controlSocket, data[5:5+clipboardLength])
									if err != nil {
										return
									}
									if n != clipboardLength {
										return
									}

									lineBytes, err := json.Marshal(string(data[5 : 5+clipboardLength]))
									if err != nil {
										panic(err)
									}

									go func(line string) {
										ps.clipboardChannel <- line
									}(string(lineBytes))
								case 1:
									n, err = io.ReadFull(ps.controlSocket, data[1:9])
									if err != nil {
										return
									}
									if n != 8 {
										return
									}

									go func(line string) {
										ps.clipboardChannel <- line
									}(strconv.FormatUint(binary.BigEndian.Uint64(data[1:9]), 10))
								case 2:
									n, err = io.ReadFull(ps.controlSocket, data[1:5])
									if err != nil {
										return
									}
									if n != 4 {
										return
									}

									size := int(binary.BigEndian.Uint16(data[3:5]))

									n, err = io.ReadFull(ps.controlSocket, data[5:5+size])
									if err != nil {
										return
									}
									if n != size {
										return
									}

									if int(binary.BigEndian.Uint16(data[1:3])) == 1 {
										select {
										case ps.uhidKeyboardOutputChannel <- hex.EncodeToString(data[5 : 5+size]):
										default:
										}
									}
								}
							}
						}()
					}

					if config.Ports[p].Video {
						ps.videoConnectedChannel <- struct{}{}
					}

					if config.Ports[p].Audio {
						ps.audioConnectedChannel <- struct{}{}
					}

					if len(ps.connectedCommands) > 0 {
						go runCommands(ps, p, ps.connectedCommands)
					}
				} else {
					if ps.videoSocket != nil {
						ps.videoSocket.Close()
					}

					if ps.audioSocket != nil {
						ps.audioSocket.Close()
					}

					if ps.controlSocket != nil {
						ps.controlSocket.Close()
					}
				}
			}
		}(port)
	}

	for endpointPath, endpoint := range config.Endpoints {
		if strings.TrimSpace(endpointPath) !=
			endpointPath || !strings.HasPrefix(endpointPath, "/") ||
			strings.HasSuffix(endpointPath, "/") {
			os.Exit(1)
		}

		if len(endpoint.Commands) > 0 && endpoint.Response != "" {
			os.Exit(1)
		}

		if endpoint.Response != "" && endpoint.Response != "videoStream" && endpoint.Response != "rawVideoStream" && endpoint.Response != "audioStream" && endpoint.Response != "rawAudioStream" && endpoint.Response != "clipboardStream" && endpoint.Response != "uhidKeyboardOutputStream" && endpoint.Response != "clipboard" && endpoint.Response != "deviceName" && endpoint.Response != "videoCodec" && endpoint.Response != "audioCodec" && endpoint.Response != "initialVideoWidth" && endpoint.Response != "initialVideoHeight" && endpoint.Response != "videoFrame" && endpoint.Response != "encoders" && endpoint.Response != "displays" && endpoint.Response != "cameras" && endpoint.Response != "cameraSizes" && endpoint.Response != "apps" {
			os.Exit(1)
		}

		if endpoint.Response == "clipboard" && endpoint.ClipboardTimeout < 1 {
			os.Exit(1)
		}

		http.HandleFunc(endpointPath,
			endpointHandler)
	}

	loadExtensions()

	if config.Static != "" {
		http.Handle("/", http.FileServer(http.Dir(config.Static)))
	}

	if config.Cert == "" && config.Key == "" {
		http.ListenAndServe(config.Address, nil)
	} else {
		http.ListenAndServeTLS(config.Address, config.Cert, config.Key, nil)
	}
}
