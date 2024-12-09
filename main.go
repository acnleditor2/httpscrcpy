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
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Endpoint struct {
	Commands         [][]string `json:"commands"`
	Response         string     `json:"response"`
	ClipboardCut     bool       `json:"clipboardCut"`
	ClipboardTimeout int        `json:"clipboardTimeout"`
}

type Config struct {
	HttpServer struct {
		Enabled   bool                `json:"enabled"`
		Address   string              `json:"address"`
		Static    string              `json:"static"`
		Cert      string              `json:"cert"`
		Key       string              `json:"key"`
		Endpoints map[string]Endpoint `json:"endpoints"`
	} `json:"httpServer"`

	StdinCommands struct {
		Enabled bool `json:"enabled"`
	} `json:"stdinCommands"`

	Adb struct {
		Enabled    bool     `json:"enabled"`
		Executable string   `json:"executable"`
		Options    []string `json:"options"`
		Device     string   `json:"device"`
	} `json:"adb"`

	Scrcpy struct {
		Enabled                  bool       `json:"enabled"`
		Port                     int        `json:"port"`
		Video                    bool       `json:"video"`
		Audio                    bool       `json:"audio"`
		Control                  bool       `json:"control"`
		Forward                  bool       `json:"forward"`
		UhidKeyboardReportDesc   string     `json:"uhidKeyboardReportDesc"`
		UhidKeyboardName         string     `json:"uhidKeyboardName"`
		UhidKeyboardVendorId     string     `json:"uhidKeyboardVendorId"`
		UhidKeyboardProductId    string     `json:"uhidKeyboardProductId"`
		UhidMouseReportDesc      string     `json:"uhidMouseReportDesc"`
		UhidMouseName            string     `json:"uhidMouseName"`
		UhidMouseVendorId        string     `json:"uhidMouseVendorId"`
		UhidMouseProductId       string     `json:"uhidMouseProductId"`
		UhidGamepadReportDesc    string     `json:"uhidGamepadReportDesc"`
		UhidGamepadName          string     `json:"uhidGamepadName"`
		UhidGamepadVendorId      string     `json:"uhidGamepadVendorId"`
		UhidGamepadProductId     string     `json:"uhidGamepadProductId"`
		StdoutClipboard          bool       `json:"stdoutClipboard"`
		StdoutUhidKeyboardOutput bool       `json:"stdoutUhidKeyboardOutput"`
		ConnectedCommands        [][]string `json:"connectedCommands"`
		Server                   string     `json:"server"`
		ServerVersion            string     `json:"serverVersion"`
		ServerOptions            []string   `json:"serverOptions"`
		ClipboardAutosync        bool       `json:"clipboardAutosync"`
		Cleanup                  bool       `json:"cleanup"`
		PowerOn                  bool       `json:"powerOn"`
	} `json:"scrcpy"`

	VideoDecoder struct {
		Enabled    bool   `json:"enabled"`
		Executable string `json:"executable"`
		Stream     bool   `json:"stream"`
		Alpha      bool   `json:"alpha"`
	} `json:"videoDecoder"`
}

var stdinDecoder *json.Decoder
var config Config
var listener net.Listener
var videoSocket net.Conn
var audioSocket net.Conn
var controlSocket net.Conn
var connectionControlChannel chan bool = make(chan bool)
var videoConnectedChannel chan struct{} = make(chan struct{})
var audioConnectedChannel chan struct{} = make(chan struct{})
var clipboardChannel chan string = make(chan string)
var uhidKeyboardOutputChannel chan string = make(chan string)
var deviceName string
var videoCodec uint32
var audioCodec uint32
var initialVideoWidth int
var initialVideoHeight int
var scrcpyServer *exec.Cmd
var scrcpyConnectedCommands [][]string
var videoDecoderIsFfmpeg bool
var videoFrame []byte
var videoFrameWidth int
var videoFrameHeight int
var videoFrameMutex sync.RWMutex

func list(serverArg string) (string, int) {
	if !config.Adb.Enabled || !config.Scrcpy.Enabled || config.Scrcpy.Server == "" || config.Scrcpy.ServerVersion == "" {
		return "", http.StatusNotFound
	}

	var args []string
	if config.Adb.Device == "usb" {
		args = append(config.Adb.Options, "-d")
	} else if config.Adb.Device == "tcpip" {
		args = append(config.Adb.Options, "-e")
	} else if config.Adb.Device != "" {
		args = append(config.Adb.Options, "-s", config.Adb.Device)
	} else {
		args = config.Adb.Options
	}

	args = append(
		args,
		"shell",
		fmt.Sprintf("CLASSPATH=%s", config.Scrcpy.Server),
		"app_process",
		"/",
		"com.genymobile.scrcpy.Server",
		config.Scrcpy.ServerVersion,
		serverArg,
	)

	if !config.Scrcpy.Cleanup {
		args = append(args, "cleanup=false")
	}

	output, err := exec.Command(config.Adb.Executable, args...).CombinedOutput()
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

func readDeviceMeta() bool {
	data := make([]byte, 64)
	var n int
	var err error

	if config.Scrcpy.Video {
		n, err = io.ReadFull(videoSocket, data)
	} else if config.Scrcpy.Audio {
		n, err = io.ReadFull(audioSocket, data)
	} else {
		n, err = io.ReadFull(controlSocket, data)
	}

	if err != nil {
		return false
	}

	if n != 64 {
		return false
	}

	deviceName = string(data[:bytes.IndexByte(data, 0)])

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

		endpoint := config.HttpServer.Endpoints[req.URL.Path]

		if len(endpoint.Commands) > 0 {
			query := req.URL.Query()
			commands := make([][]string, len(endpoint.Commands))
			for i := range endpoint.Commands {
				commands[i] = make([]string, len(endpoint.Commands[i]))
				for j := range endpoint.Commands[i] {
					commands[i][j] = os.Expand(endpoint.Commands[i][j], query.Get)
				}
			}

			go commandsRun(commands)
			w.WriteHeader(http.StatusNoContent)
		} else {
			switch endpoint.Response {
			case "videoStream":
				videoSendStream(w, req, true)
			case "rawVideoStream":
				videoSendStream(w, req, false)
			case "rgbVideoStream":
				if videoDecoderIsFfmpeg {
					videoSendFfmpegRgbStream(w, req)
				} else {
					videoSendRgbStream(w, req)
				}
			case "audioStream":
				audioSendStream(w, req, true)
			case "rawAudioStream":
				audioSendStream(w, req, false)
			case "clipboardStream":
				clipboardSendStream(w, req)
			case "uhidKeyboardOutputStream":
				inputUhidKeyboardSendOutputStream(w, req)
			case "clipboard":
				if controlSocket == nil {
					w.WriteHeader(http.StatusNotFound)
					return
				}

				var text string
				status := clipboardGet(endpoint.ClipboardCut, &text, time.Duration(endpoint.ClipboardTimeout)*time.Millisecond)

				if status != http.StatusOK {
					w.WriteHeader(status)
					return
				}

				w.Write([]byte(text))
			case "deviceName":
				if deviceName == "" {
					w.WriteHeader(http.StatusNotFound)
				} else {
					w.Write([]byte(deviceName))
				}
			case "videoCodec":
				if videoCodec == 0 {
					w.WriteHeader(http.StatusNotFound)
				} else {
					w.Write([]byte(strconv.FormatUint(uint64(videoCodec), 10)))
				}
			case "audioCodec":
				if audioCodec == 0 {
					w.WriteHeader(http.StatusNotFound)
				} else {
					w.Write([]byte(strconv.FormatUint(uint64(audioCodec), 10)))
				}
			case "initialVideoWidth":
				if initialVideoWidth == 0 {
					w.WriteHeader(http.StatusNotFound)
				} else {
					w.Write([]byte(strconv.Itoa(initialVideoWidth)))
				}
			case "initialVideoHeight":
				if initialVideoHeight == 0 {
					w.WriteHeader(http.StatusNotFound)
				} else {
					w.Write([]byte(strconv.Itoa(initialVideoHeight)))
				}
			case "videoFrame":
				videoSendFrame(w, req)
			case "encoders", "displays", "cameras", "apps":
				output, status := list(fmt.Sprintf("list_%s=true", endpoint.Response))

				if status != http.StatusOK {
					w.WriteHeader(status)
				}

				if output != "" {
					w.Write([]byte(output))
				}
			case "cameraSizes":
				output, status := list("list_camera_sizes=true")

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

	var err error

	if len(os.Args) == 1 || os.Args[1] == "-" {
		stdinDecoder = json.NewDecoder(os.Stdin)
		err = stdinDecoder.Decode(&config)
	} else if strings.HasPrefix(os.Args[1], "http://") || strings.HasPrefix(os.Args[1], "https://") {
		response, err := http.Get(os.Args[1])
		if err != nil {
			panic(err)
		}

		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			os.Exit(1)
		}

		err = json.NewDecoder(response.Body).Decode(&config)
		response.Body.Close()
	} else {
		configFile, err := os.Open(os.Args[1])
		if err != nil {
			panic(err)
		}

		err = json.NewDecoder(configFile).Decode(&config)
		configFile.Close()
	}

	if err != nil {
		panic(err)
	}

	if !config.Adb.Enabled && !config.Scrcpy.Enabled {
		os.Exit(1)
	}

	if !config.HttpServer.Enabled && !config.StdinCommands.Enabled {
		os.Exit(1)
	}

	if config.Adb.Enabled && config.Adb.Executable == "" {
		os.Exit(1)
	}

	if config.Scrcpy.Enabled && config.Scrcpy.Port < 1 {
		os.Exit(1)
	}

	if config.VideoDecoder.Enabled && (!config.Scrcpy.Enabled || config.VideoDecoder.Executable == "") {
		os.Exit(1)
	}

	if config.HttpServer.Enabled && (config.HttpServer.Address == "" || len(config.HttpServer.Endpoints) == 0) {
		os.Exit(1)
	}

	if config.Scrcpy.Enabled {
		scrcpyConnectedCommands = config.Scrcpy.ConnectedCommands

		if config.Scrcpy.Video && config.VideoDecoder.Enabled && !config.VideoDecoder.Stream {
			if runtime.GOOS == "windows" {
				videoDecoderIsFfmpeg = true
			} else {
				_, ok := exec.Command(config.VideoDecoder.Executable).Run().(*exec.ExitError)
				videoDecoderIsFfmpeg = ok
			}

			if videoDecoderIsFfmpeg {
				go videoDecodeFfmpeg()
			} else {
				go videoDecode()
			}
		}

		go func() {
			var err error

			if !config.Scrcpy.Forward {
				listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", config.Scrcpy.Port))
				if err != nil {
					return
				}
			}

			for connect := range connectionControlChannel {
				if connect {
					if config.Scrcpy.Forward {
						var connected bool

						for i := 0; i < 100; i++ {
							if videoSocket != nil {
								videoSocket.Close()
							}

							if audioSocket != nil {
								audioSocket.Close()
							}

							if controlSocket != nil {
								controlSocket.Close()
							}

							if i != 0 {
								time.Sleep(100 * time.Millisecond)
							}

							if config.Scrcpy.Video {
								videoSocket, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", config.Scrcpy.Port))
								if err != nil {
									break
								}

								if !readDummyByte(videoSocket) {
									continue
								}
							}

							if config.Scrcpy.Audio {
								audioSocket, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", config.Scrcpy.Port))
								if err != nil {
									break
								}

								if !config.Scrcpy.Video && !readDummyByte(audioSocket) {
									continue
								}
							}

							if config.Scrcpy.Control {
								controlSocket, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", config.Scrcpy.Port))
								if err != nil {
									break
								}

								if !config.Scrcpy.Video && !config.Scrcpy.Audio && !readDummyByte(controlSocket) {
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
						if videoSocket != nil {
							videoSocket.Close()
						}

						if audioSocket != nil {
							audioSocket.Close()
						}

						if controlSocket != nil {
							controlSocket.Close()
						}

						if config.Scrcpy.Video {
							videoSocket, err = listener.Accept()
							if err != nil {
								return
							}
						}

						if config.Scrcpy.Audio {
							audioSocket, err = listener.Accept()
							if err != nil {
								return
							}
						}

						if config.Scrcpy.Control {
							controlSocket, err = listener.Accept()
							if err != nil {
								return
							}
						}
					}

					if !readDeviceMeta() {
						continue
					}

					if config.Scrcpy.Video {
						data := make([]byte, 12)
						n, err := io.ReadFull(videoSocket, data)
						if err != nil {
							continue
						}
						if n != 12 {
							continue
						}

						videoCodec = binary.BigEndian.Uint32(data[:4])
						initialVideoWidth = int(binary.BigEndian.Uint32(data[4:8]))
						initialVideoHeight = int(binary.BigEndian.Uint32(data[8:]))
					}

					if config.Scrcpy.Audio {
						data := make([]byte, 4)
						n, err := io.ReadFull(audioSocket, data)
						if err != nil {
							continue
						}
						if n != 4 {
							continue
						}

						audioCodec = binary.BigEndian.Uint32(data)
					}

					if config.Scrcpy.Control {
						if config.Scrcpy.UhidKeyboardReportDesc != "" {
							if !inputUhidCreateDevice(config.Scrcpy.UhidKeyboardReportDesc, 0x01, config.Scrcpy.UhidKeyboardName, config.Scrcpy.UhidKeyboardVendorId, config.Scrcpy.UhidKeyboardProductId, controlSocket) {
								go func() { connectionControlChannel <- false }()
								continue
							}
						}

						if config.Scrcpy.UhidMouseReportDesc != "" {
							if !inputUhidCreateDevice(config.Scrcpy.UhidMouseReportDesc, 0x02, config.Scrcpy.UhidMouseName, config.Scrcpy.UhidMouseVendorId, config.Scrcpy.UhidMouseProductId, controlSocket) {
								go func() { connectionControlChannel <- false }()
								continue
							}
						}

						if config.Scrcpy.UhidGamepadReportDesc != "" {
							if !inputUhidCreateDevice(config.Scrcpy.UhidGamepadReportDesc, 0x03, config.Scrcpy.UhidGamepadName, config.Scrcpy.UhidGamepadVendorId, config.Scrcpy.UhidGamepadProductId, controlSocket) {
								go func() { connectionControlChannel <- false }()
								continue
							}
						}

						go func() {
							data := make([]byte, 262130)

							for {
								n, err := io.ReadFull(controlSocket, data[:1])
								if err != nil {
									return
								}
								if n != 1 {
									return
								}

								switch data[0] {
								case 0:
									n, err = io.ReadFull(controlSocket, data[:4])
									if err != nil {
										return
									}
									if n != 4 {
										return
									}

									clipboardLength := int(binary.BigEndian.Uint32(data[:4]))

									n, err = io.ReadFull(controlSocket, data[:clipboardLength])
									if err != nil {
										return
									}
									if n != clipboardLength {
										return
									}

									lineBytes, err := json.Marshal(string(data[:clipboardLength]))
									if err != nil {
										panic(err)
									}

									if config.Scrcpy.StdoutClipboard {
										fmt.Println(string(lineBytes))
									} else if config.HttpServer.Enabled {
										go func(line string) {
											clipboardChannel <- line
										}(string(lineBytes))
									}
								case 1:
									n, err = io.ReadFull(controlSocket, data[:8])
									if err != nil {
										return
									}
									if n != 8 {
										return
									}

									if config.Scrcpy.StdoutClipboard {
										fmt.Println(strconv.FormatUint(binary.BigEndian.Uint64(data[:8]), 10))
									} else if config.HttpServer.Enabled {
										go func(line string) {
											clipboardChannel <- line
										}(strconv.FormatUint(binary.BigEndian.Uint64(data[:8]), 10))
									}
								case 2:
									n, err = io.ReadFull(controlSocket, data[:4])
									if err != nil {
										return
									}
									if n != 4 {
										return
									}

									size := int(binary.BigEndian.Uint16(data[:4]))

									n, err = io.ReadFull(controlSocket, data[:size])
									if err != nil {
										return
									}
									if n != size {
										return
									}

									if int(binary.BigEndian.Uint16(data[1:3])) == 1 {
										if config.Scrcpy.StdoutUhidKeyboardOutput {
											fmt.Println(hex.EncodeToString(data[:size]))
										} else if config.HttpServer.Enabled {
											select {
											case uhidKeyboardOutputChannel <- hex.EncodeToString(data[:size]):
											default:
											}
										}
									}
								}
							}
						}()
					}

					if config.Scrcpy.Video {
						videoConnectedChannel <- struct{}{}
					}

					if config.Scrcpy.Audio {
						audioConnectedChannel <- struct{}{}
					}

					if len(scrcpyConnectedCommands) > 0 {
						go commandsRun(scrcpyConnectedCommands)
					}
				} else {
					if videoSocket != nil {
						videoSocket.Close()
					}

					if audioSocket != nil {
						audioSocket.Close()
					}

					if controlSocket != nil {
						controlSocket.Close()
					}
				}
			}
		}()
	}

	if config.HttpServer.Enabled {
		for endpointPath, endpoint := range config.HttpServer.Endpoints {
			if strings.TrimSpace(endpointPath) !=
				endpointPath || !strings.HasPrefix(endpointPath, "/") ||
				strings.HasSuffix(endpointPath, "/") {
				os.Exit(1)
			}

			if len(endpoint.Commands) > 0 && endpoint.Response != "" {
				os.Exit(1)
			}

			if endpoint.Response != "" && endpoint.Response != "videoStream" && endpoint.Response != "rawVideoStream" && endpoint.Response != "rgbVideoStream" && endpoint.Response != "audioStream" && endpoint.Response != "rawAudioStream" && endpoint.Response != "clipboardStream" && endpoint.Response != "uhidKeyboardOutputStream" && endpoint.Response != "clipboard" && endpoint.Response != "deviceName" && endpoint.Response != "videoCodec" && endpoint.Response != "audioCodec" && endpoint.Response != "initialVideoWidth" && endpoint.Response != "initialVideoHeight" && endpoint.Response != "videoFrame" && endpoint.Response != "encoders" && endpoint.Response != "displays" && endpoint.Response != "cameras" && endpoint.Response != "cameraSizes" && endpoint.Response != "apps" {
				os.Exit(1)
			}

			if endpoint.Response == "clipboard" && endpoint.ClipboardTimeout < 1 {
				os.Exit(1)
			}

			http.HandleFunc(endpointPath,
				endpointHandler)
		}

		if config.HttpServer.Static != "" {
			http.Handle("/", http.FileServer(http.Dir(config.HttpServer.Static)))
		}

		if config.HttpServer.Cert == "" && config.HttpServer.Key == "" {
			go http.ListenAndServe(config.HttpServer.Address, nil)
		} else {
			go http.ListenAndServeTLS(config.HttpServer.Address, config.HttpServer.Cert, config.HttpServer.Key, nil)
		}
	}

	if config.StdinCommands.Enabled {
		go func() {
			if stdinDecoder == nil {
				stdinDecoder = json.NewDecoder(os.Stdin)
			}

			var c [][]string

			for {
				err = stdinDecoder.Decode(&c)
				if err != nil {
					if err == io.EOF {
						break
					} else {
						stdinDecoder = json.NewDecoder(os.Stdin)
					}

					fmt.Fprintln(os.Stderr, err)
				} else if len(c) > 0 {
					commandsRun(c)
				}
			}
		}()
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	<-interrupt

	select {
	case connectionControlChannel <- false:
	default:
	}
}
