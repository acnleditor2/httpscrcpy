package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type extensionState struct {
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	stdout         io.ReadCloser
	singleEndpoint bool
	mutex          sync.Mutex
}

var (
	extensionMap         = map[string]*extensionState{}
	endpointExtensionMap = map[string]string{}
)

func extensionRequestHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodOptions {
		w.Header().Set("Allow", "OPTIONS, GET")
		return
	} else if req.Method != http.MethodGet {
		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	query := req.URL.Query()
	extension := extensionMap[endpointExtensionMap[req.URL.Path]]

	var b bytes.Buffer

	b.WriteByte(0)

	if !extension.singleEndpoint {
		b.WriteByte(byte(len(req.URL.Path)))
		b.WriteString(req.URL.Path)
	}

	binary.Write(&b, binary.NativeEndian, uint32(len(query)))
	for name, values := range query {
		binary.Write(&b, binary.NativeEndian, uint32(len(name)))
		b.WriteString(name)
		binary.Write(&b, binary.NativeEndian, uint32(len(values[0])))
		b.WriteString(values[0])
	}

	binary.Write(&b, binary.NativeEndian, uint32(len(req.Header)))
	for name, values := range req.Header {
		binary.Write(&b, binary.NativeEndian, uint32(len(name)))
		b.WriteString(name)
		binary.Write(&b, binary.NativeEndian, uint32(len(values[0])))
		b.WriteString(values[0])
	}

	extension.mutex.Lock()
	defer extension.mutex.Unlock()

	_, err := b.WriteTo(extension.stdin)
	if err != nil {
		return
	}

	data := make([]byte, 3)

	n, err := io.ReadFull(extension.stdout, data)
	if err != nil {
		return
	}
	if n != 3 {
		return
	}

	status := int(binary.NativeEndian.Uint16(data[:2]))
	headerCount := int(data[2])

	for i := 0; i < headerCount; i++ {
		n, err = io.ReadFull(extension.stdout, data[:1])
		if err != nil {
			return
		}
		if n != 1 || data[0] < 1 {
			return
		}

		data = make([]byte, int(data[0]))

		n, err = io.ReadFull(extension.stdout, data)
		if err != nil {
			return
		}
		if n != len(data) {
			return
		}

		name := string(data)

		n, err = io.ReadFull(extension.stdout, data[:1])
		if err != nil {
			return
		}
		if n != 1 || data[0] < 1 {
			return
		}

		data = make([]byte, int(data[0]))

		n, err = io.ReadFull(extension.stdout, data)
		if err != nil {
			return
		}
		if n != len(data) {
			return
		}

		w.Header().Set(name, string(data))
	}

	w.WriteHeader(status)

	for {
		data = make([]byte, 4)

		n, err = io.ReadFull(extension.stdout, data)
		if err != nil {
			return
		}
		if n != 4 {
			return
		}

		size := int(binary.NativeEndian.Uint32(data))

		if size == 0 {
			break
		}

		data = make([]byte, size)

		n, err = io.ReadFull(extension.stdout, data)
		if err != nil {
			return
		}
		if n != size {
			return
		}

		w.Write(data)
		w.(http.Flusher).Flush()
	}

	data = make([]byte, 1)

	n, err = io.ReadFull(extension.stdout, data)
	if err != nil {
		return
	}
	if n != 1 {
		return
	}

	commands := make([][]string, int(data[0]))

	for i := 0; i < len(commands); i++ {
		n, err = io.ReadFull(extension.stdout, data[:1])
		if err != nil {
			return
		}
		if n != 1 || data[0] < 1 {
			return
		}

		commands[i] = make([]string, int(data[0]))

		for j := 0; j < len(commands[i]); j++ {
			n, err = io.ReadFull(extension.stdout, data[:1])
			if err != nil {
				return
			}
			if n != 1 {
				return
			}

			if data[0] > 0 {
				data = make([]byte, int(data[0]))

				n, err = io.ReadFull(extension.stdout, data)
				if err != nil {
					return
				}
				if n != len(data) {
					return
				}

				commands[i][j] = string(data)
			}
		}
	}

	if len(commands) > 0 {
		var port int

		data = make([]byte, 2)

		n, err = io.ReadFull(extension.stdout, data)
		if err != nil {
			return
		}
		if n != 2 {
			return
		}

		if len(config.Ports) == 1 {
			for p := range config.Ports {
				port = p
			}
		} else {
			port = int(binary.NativeEndian.Uint16(data))
		}

		ps, ok := portMap[port]
		if !ok {
			return
		}

		go runCommands(ps, port, commands)
	}
}

func loadExtensions() {
	for _, extension := range config.Extensions {
		cmd := exec.Command(extension[0], extension[1:]...)
		cmd.Stderr = os.Stderr

		stdin, err := cmd.StdinPipe()
		if err != nil {
			panic(err)
		}

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			panic(err)
		}

		err = cmd.Start()
		if err != nil {
			panic(err)
		}

		data := make([]byte, 1)

		n, err := io.ReadFull(stdout, data)
		if err != nil {
			panic(err)
		}
		if n != 1 || data[0] < 1 {
			cmd.Process.Kill()
			cmd.Wait()
			os.Exit(1)
		}

		data = make([]byte, int(data[0]))

		n, err = io.ReadFull(stdout, data)
		if err != nil {
			panic(err)
		}
		if n != len(data) {
			cmd.Process.Kill()
			cmd.Wait()
			os.Exit(1)
		}

		extensionId := string(data)

		{
			_, ok := extensionMap[extensionId]
			if ok {
				cmd.Process.Kill()
				cmd.Wait()
				os.Exit(1)
			}
		}

		data = make([]byte, 1)

		n, err = io.ReadFull(stdout, data)
		if err != nil {
			panic(err)
		}
		if n != 1 {
			cmd.Process.Kill()
			cmd.Wait()
			os.Exit(1)
		}

		endpointCount := int(data[0])

		if endpointCount < 1 {
			cmd.Process.Kill()
			cmd.Wait()
			os.Exit(1)
		}

		for i := 0; i < endpointCount; i++ {
			data = make([]byte, 1)

			n, err = io.ReadFull(stdout, data)
			if err != nil {
				panic(err)
			}
			if n != 1 || data[0] < 2 {
				cmd.Process.Kill()
				cmd.Wait()
				os.Exit(1)
			}

			data = make([]byte, int(data[0]))

			n, err = io.ReadFull(stdout, data)
			if err != nil {
				panic(err)
			}
			if n != len(data) {
				cmd.Process.Kill()
				cmd.Wait()
				os.Exit(1)
			}

			s := strings.TrimSpace(string(data))

			if !strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") {
				cmd.Process.Kill()
				cmd.Wait()
				os.Exit(1)
			}

			http.HandleFunc(s, extensionRequestHandler)
			endpointExtensionMap[s] = extensionId
		}

		extensionMap[extensionId] = &extensionState{
			cmd:            cmd,
			stdin:          stdin,
			stdout:         stdout,
			singleEndpoint: endpointCount == 1,
		}
	}

	for portNumber, port := range config.Ports {
		if port.Video && port.VideoExtension != "" {
			extension, ok := extensionMap[port.VideoExtension]
			if ok {
				go sendVideoToExtension(portNumber, portMap[portNumber], extension)
			}
		}

		if port.Audio && port.AudioExtension != "" {
			extension, ok := extensionMap[port.AudioExtension]
			if ok {
				go sendAudioToExtension(portNumber, portMap[portNumber], extension)
			}
		}

		if port.Control {
			if port.ClipboardStreamExtension != "" {
				extension, ok := extensionMap[port.ClipboardStreamExtension]
				if ok {
					go sendClipboardToExtension(portNumber, portMap[portNumber], extension)
				}
			}

			if port.UhidKeyboardReportDesc != "" && port.UhidKeyboardOutputExtension != "" {
				extension, ok := extensionMap[port.UhidKeyboardOutputExtension]
				if ok {
					go sendUhidKeyboardOutputToExtension(portNumber, portMap[portNumber], extension)
				}
			}
		}
	}
}
