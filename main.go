package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/template"
)

type portState struct {
	video                    bool
	audio                    bool
	control                  bool
	forward                  bool
	listener                 net.Listener
	videoSocket              net.Conn
	audioSocket              net.Conn
	controlSocket            net.Conn
	connectionControlChannel chan bool
	videoConnectedChannel    chan struct{}
	audioConnectedChannel    chan struct{}
	clipboardChannel         chan string
	sendVideoSocket          net.Conn
	sendAudioSocket          net.Conn
	deviceName               string
	videoCodec               uint32
	audioCodec               uint32
	initialVideoWidth        uint32
	initialVideoHeight       uint32
}

type Port struct {
	Video   bool `json:"video"`
	Audio   bool `json:"audio"`
	Control bool `json:"control"`
	Forward bool `json:"forward"`
}

type Config struct {
	Address       string            `json:"address"`
	Static        string            `json:"static"`
	Cert          string            `json:"cert"`
	Key           string            `json:"key"`
	Ports         map[string]Port   `json:"ports"`
	Scripts       map[string]string `json:"scripts"`
	CachedScripts []string          `json:"cachedScripts"`
	Users         map[string]User   `json:"users"`
}

var portMap = map[int]*portState{}
var config Config

func getPort(portString string) int {
	if portString == "" && len(portMap) == 1 {
		for port := range portMap {
			return port
		}
	}

	port, err := strconv.Atoi(portString)
	if err != nil {
		return -1
	}

	return port
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

func readDeviceMeta(ps *portState) bool {
	data := make([]byte, 64)
	var n int
	var err error

	if ps.video {
		n, err = io.ReadFull(ps.videoSocket, data)
	} else if ps.audio {
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

func connectHandler(w http.ResponseWriter, req *http.Request) {
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
			if user.ScriptOnly {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}

		port := getPort(req.URL.Query().Get("port"))
		if port == -1 {
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

		w.WriteHeader(runCommand(ps, port, []string{"connect"}))
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func disconnectHandler(w http.ResponseWriter, req *http.Request) {
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
			if user.ScriptOnly {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}

		port := getPort(req.URL.Query().Get("port"))
		if port == -1 {
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

		w.WriteHeader(runCommand(ps, port, []string{"disconnect"}))
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func portInfoHandler(w http.ResponseWriter, req *http.Request) {
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
			if user.ScriptOnly {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}

		port := getPort(req.URL.Query().Get("port"))
		if port == -1 {
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

		switch req.URL.Path {
		case "/device-name":
			if ps.deviceName == "" {
				w.WriteHeader(http.StatusNotFound)
			} else {
				w.Write([]byte(ps.deviceName))
			}
		case "/video-codec":
			if ps.videoCodec == 0 {
				w.WriteHeader(http.StatusNotFound)
			} else {
				w.Write([]byte(strconv.FormatUint(uint64(ps.videoCodec), 10)))
			}
		case "/audio-codec":
			if ps.audioCodec == 0 {
				w.WriteHeader(http.StatusNotFound)
			} else {
				w.Write([]byte(strconv.FormatUint(uint64(ps.audioCodec), 10)))
			}
		case "/initial-video-width":
			if ps.initialVideoWidth == 0 {
				w.WriteHeader(http.StatusNotFound)
			} else {
				w.Write([]byte(strconv.FormatUint(uint64(ps.initialVideoWidth), 10)))
			}
		case "/initial-video-height":
			if ps.initialVideoHeight == 0 {
				w.WriteHeader(http.StatusNotFound)
			} else {
				w.Write([]byte(strconv.FormatUint(uint64(ps.initialVideoHeight), 10)))
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

func allowedPortsHandler(w http.ResponseWriter, req *http.Request) {
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
			if user.ScriptOnly {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}

		ports := map[string]Port{}

		for port := range portMap {
			if len(config.Users) == 0 || portAllowedForUser(port, username) {
				portString := strconv.Itoa(port)
				ports[portString] = config.Ports[portString]
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ports)
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func sendDataHandler(w http.ResponseWriter, req *http.Request) {
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
			if user.ScriptOnly {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}

		query := req.URL.Query()

		port := getPort(query.Get("port"))
		if port == -1 {
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

		if !ps.control || ps.controlSocket == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(runCommand(ps, port, []string{"senddata", query.Get("data")}))
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
	var configBytes []byte
	var err error

	if len(os.Args) < 2 || os.Args[1] == "-" {
		configBytes, err = io.ReadAll(os.Stdin)
	} else if strings.HasPrefix(os.Args[1], "http://") || strings.HasPrefix(os.Args[1], "https://") {
		r, err := http.Get(os.Args[1])
		if err != nil {
			panic(err)
		}

		defer r.Body.Close()

		if r.StatusCode != http.StatusOK {
			os.Exit(1)
		}

		configBytes, err = io.ReadAll(r.Body)
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

	for portString, portInfo := range config.Ports {
		port, err := strconv.Atoi(portString)
		if err != nil {
			panic(err)
		}

		portMap[port] = &portState{
			video:                    portInfo.Video,
			audio:                    portInfo.Audio,
			control:                  portInfo.Control,
			forward:                  portInfo.Forward,
			connectionControlChannel: make(chan bool),
			videoConnectedChannel:    make(chan struct{}),
			audioConnectedChannel:    make(chan struct{}),
			clipboardChannel:         make(chan string),
		}

		go func(p int) {
			ps := portMap[p]
			var err error

			if !ps.forward {
				ps.listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
				if err != nil {
					panic(err)
				}
				defer ps.listener.Close()
			}

			for connect := range ps.connectionControlChannel {
				if ps.videoSocket != nil {
					ps.videoSocket.Close()
				}

				if ps.audioSocket != nil {
					ps.audioSocket.Close()
				}

				if ps.controlSocket != nil {
					ps.controlSocket.Close()
				}

				if connect {
					if ps.video {
						if ps.forward {
							ps.videoSocket, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", p))
							if err != nil {
								panic(err)
							}

							if !readDummyByte(ps.videoSocket) {
								continue
							}
						} else {
							ps.videoSocket, err = ps.listener.Accept()
							if err != nil {
								panic(err)
							}
						}
					}

					if ps.audio {
						if ps.forward {
							ps.audioSocket, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", p))
							if err != nil {
								panic(err)
							}

							if !ps.video && !readDummyByte(ps.audioSocket) {
								continue
							}
						} else {
							ps.audioSocket, err = ps.listener.Accept()
							if err != nil {
								panic(err)
							}
						}
					}

					if ps.control {
						if ps.forward {
							ps.controlSocket, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", p))
							if err != nil {
								panic(err)
							}

							if !ps.video && !ps.audio && !readDummyByte(ps.controlSocket) {
								continue
							}
						} else {
							ps.controlSocket, err = ps.listener.Accept()
							if err != nil {
								panic(err)
							}
						}
					}

					if !readDeviceMeta(ps) {
						continue
					}

					if ps.video {
						data := make([]byte, 12)
						n, err := io.ReadFull(ps.videoSocket, data)
						if err != nil {
							continue
						}
						if n != 12 {
							continue
						}

						ps.videoCodec = binary.BigEndian.Uint32(data[:4])
						ps.initialVideoWidth = binary.BigEndian.Uint32(data[4:8])
						ps.initialVideoHeight = binary.BigEndian.Uint32(data[8:])
					}

					if ps.audio {
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

					if ps.control {
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

									ps.clipboardChannel <- string(lineBytes)
								case 1:
									n, err = io.ReadFull(ps.controlSocket, data[1:9])
									if err != nil {
										return
									}
									if n != 8 {
										return
									}

									ps.clipboardChannel <- strconv.FormatUint(binary.BigEndian.Uint64(data[1:9]), 10)
								}
							}
						}()
					}

					if ps.video {
						ps.videoConnectedChannel <- struct{}{}
					}

					if ps.audio {
						ps.audioConnectedChannel <- struct{}{}
					}
				}
			}
		}(port)
	}

	for script := range config.Scripts {
		scriptMessageChannelMap[script] = make(chan string)
	}

	for _, script := range config.CachedScripts {
		_, ok := config.Scripts[script]
		if ok {
			t := template.Must(getScriptTemplate(script))
			if t != nil {
				cachedScriptTemplateMap[script] = t
			}
		}
	}

	var fs http.Handler
	if config.Static != "" {
		fs = http.FileServer(http.Dir(config.Static))
	}

	http.HandleFunc("/connect", connectHandler)
	http.HandleFunc("/disconnect", disconnectHandler)
	http.HandleFunc("/device-name", portInfoHandler)
	http.HandleFunc("/initial-video-width", portInfoHandler)
	http.HandleFunc("/initial-video-height", portInfoHandler)
	http.HandleFunc("/video-codec", portInfoHandler)
	http.HandleFunc("/audio-codec", portInfoHandler)
	http.HandleFunc("/allowed-ports", allowedPortsHandler)
	http.HandleFunc("/send-data", sendDataHandler)
	http.HandleFunc("/video", videoStreamHandler)
	http.HandleFunc("/audio", audioStreamHandler)
	http.HandleFunc("/clipboard", clipboardStreamHandler)
	http.HandleFunc("/key", keyHandler)
	http.HandleFunc("/key-down", keyHandler)
	http.HandleFunc("/key-up", keyHandler)
	http.HandleFunc("/type", typeHandler)
	http.HandleFunc("/touch", touchHandler)
	http.HandleFunc("/touch-down", touchHandler)
	http.HandleFunc("/touch-up", touchHandler)
	http.HandleFunc("/touch-move", touchHandler)
	http.HandleFunc("/mouse-click", mouseHandler)
	http.HandleFunc("/mouse-down", mouseHandler)
	http.HandleFunc("/mouse-up", mouseHandler)
	http.HandleFunc("/mouse-move", mouseHandler)
	http.HandleFunc("/scroll-left", scrollHandler)
	http.HandleFunc("/scroll-right", scrollHandler)
	http.HandleFunc("/scroll-up", scrollHandler)
	http.HandleFunc("/scroll-down", scrollHandler)
	http.HandleFunc("/get-clipboard", getClipboardHandler)
	http.HandleFunc("/set-clipboard", setClipboardHandler)
	http.HandleFunc("/power", keyHandler)
	http.HandleFunc("/sleep", keyHandler)
	http.HandleFunc("/wake-up", keyHandler)
	http.HandleFunc("/back", keyHandler)
	http.HandleFunc("/home", keyHandler)
	http.HandleFunc("/app-switch", keyHandler)
	http.HandleFunc("/menu", keyHandler)
	http.HandleFunc("/assist", keyHandler)
	http.HandleFunc("/voice-assist", keyHandler)
	http.HandleFunc("/all-apps", keyHandler)
	http.HandleFunc("/volume-up", keyHandler)
	http.HandleFunc("/volume-down", keyHandler)
	http.HandleFunc("/brightness-up", keyHandler)
	http.HandleFunc("/brightness-down", keyHandler)
	http.HandleFunc("/script", scriptHandler)
	http.HandleFunc("/script-message", scriptMessageHandler)
	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		origin := req.Header.Get("Origin")

		w.Header().Set("Cache-Control", "no-store")

		switch req.Method {
		case http.MethodOptions:
			if req.Header.Get("Access-Control-Request-Method") == "" {
				w.Header().Set("Allow", "OPTIONS, GET, HEAD")
			} else if origin != "" {
				requestHeaders := req.Header.Get("Access-Control-Request-Headers")

				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD")

				if requestHeaders != "" {
					w.Header().Set("Access-Control-Allow-Headers", requestHeaders)
				}
			}
		case http.MethodGet, http.MethodHead:
			if origin != "" {
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}

			if req.Method == http.MethodGet && (req.URL.Path == "/back-or-screen-on" || req.URL.Path == "/expand-notifications-panel" || req.URL.Path == "/expand-settings-panel" || req.URL.Path == "/collapse-panels" || req.URL.Path == "/turn-screen-on" || req.URL.Path == "/turn-screen-off" || req.URL.Path == "/rotate") {
				var username string
				var user *User

				if len(config.Users) > 0 {
					username, user = auth(w, req)
					if user == nil {
						return
					}
					if user.ScriptOnly {
						w.WriteHeader(http.StatusForbidden)
						return
					}
				}

				port := getPort(req.URL.Query().Get("port"))
				if port == -1 {
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

				if !ps.control || ps.controlSocket == nil {
					w.WriteHeader(http.StatusNotFound)
					return
				}

				w.WriteHeader(runCommand(ps, port, []string{strings.ReplaceAll(req.URL.Path[1:], "-", "")}))
			} else if config.Static == "" {
				w.WriteHeader(http.StatusNotFound)
			} else {
				fs.ServeHTTP(w, req)
			}
		default:
			if origin != "" {
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
			w.Header().Set("Allow", "OPTIONS, GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	if config.Cert == "" && config.Key == "" {
		http.ListenAndServe(config.Address, nil)
	} else {
		http.ListenAndServeTLS(config.Address, config.Cert, config.Key, nil)
	}
}
