package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
)

func sendVideoStream(w http.ResponseWriter, req *http.Request, port int, header bool) {
	ps, ok := portMap[port]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if !config.Ports[port].Video || (!config.Ports[port].VideoStream && (config.Ports[port].Ffmpeg != "" || config.Ffmpeg != "")) || config.Ports[port].VideoExtension != "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	select {
	case <-ps.videoConnectedChannel:
	case <-req.Context().Done():
		return
	}

	if req.Header.Get("Origin") != "" {
		w.Header().Set("Access-Control-Expose-Headers", "Device-Name, Codec, Initial-Width, Initial-Height")
	}

	w.Header().Set("Device-Name", ps.deviceName)
	w.Header().Set("Codec", strconv.FormatUint(uint64(ps.videoCodec), 10))
	w.Header().Set("Initial-Width", strconv.Itoa(ps.initialVideoWidth))
	w.Header().Set("Initial-Height", strconv.Itoa(ps.initialVideoHeight))

	headerBytes := make([]byte, 12)
	var packetSize int
	var packet []byte
	var n int
	var err error

	if header {
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
	} else {
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

func sendVideoToFfmpeg(port int, ps *portState) {
	headerBytes := make([]byte, 12)
	var err error
	var packetSize int
	var packet []byte
	var n int
	var ffmpeg *exec.Cmd
	var ffmpegStdin io.WriteCloser
	var ffmpegStdout io.ReadCloser

	for {
		<-ps.videoConnectedChannel

		videoFrameSize := ps.initialVideoWidth * ps.initialVideoHeight * map[bool]int{
			false: 3,
			true:  4,
		}[config.Ports[port].VideoFrameAlpha]

		ps.videoFrameMutex.Lock()
		if len(ps.videoFrame) != videoFrameSize {
			ps.videoFrame = make([]byte, videoFrameSize)
		}
		ps.videoFrameMutex.Unlock()

		if ffmpeg != nil {
			ffmpeg.Process.Kill()
			ffmpeg.Wait()
		}

		ffmpeg = exec.Command(
			func() string {
				if config.Ports[port].Ffmpeg != "" {
					return config.Ports[port].Ffmpeg
				}

				return config.Ffmpeg
			}(),
			"-probesize",
			"32",
			"-analyzeduration",
			"0",
			"-re",
			"-f",
			map[uint32]string{
				0x68323634: "h264",
				0x68323635: "hevc",
				0x617631:   "av1",
			}[ps.videoCodec],
			"-i",
			"-",
			"-f",
			"rawvideo",
			"-pix_fmt",
			map[bool]string{
				false: "rgb24",
				true:  "rgba",
			}[config.Ports[port].VideoFrameAlpha],
			"-vf",
			func() string {
				if ps.initialVideoWidth >= ps.initialVideoHeight {
					return "transpose=1:landscape"
				}

				return "transpose=1:portrait"
			}(),
			"-",
		)

		ffmpeg.Stderr = os.Stderr

		ffmpegStdin, err = ffmpeg.StdinPipe()
		if err != nil {
			return
		}

		ffmpegStdout, err = ffmpeg.StdoutPipe()
		if err != nil {
			return
		}

		err = ffmpeg.Start()
		if err != nil {
			return
		}

		go func() {
			ps.videoFrameMutex.RLock()
			frame := make([]byte, len(ps.videoFrame))
			ps.videoFrameMutex.RUnlock()

			for {
				n, err := io.ReadFull(ffmpegStdout, frame)
				if err != nil {
					break
				}
				if n != len(frame) {
					break
				}

				ps.videoFrameMutex.Lock()
				copy(ps.videoFrame, frame)
				ps.videoFrameMutex.Unlock()
			}
		}()

		for {
			n, err = io.ReadFull(ps.videoSocket, headerBytes)
			if err != nil {
				ffmpeg.Process.Kill()
				ffmpeg.Wait()
				ffmpeg = nil
				break
			}
			if n != 12 {
				ffmpeg.Process.Kill()
				ffmpeg.Wait()
				ffmpeg = nil
				break
			}

			packetSize = int(binary.BigEndian.Uint32(headerBytes[8:]))
			packet = make([]byte, packetSize)

			n, err = io.ReadFull(ps.videoSocket, packet)
			if err != nil {
				ffmpeg.Process.Kill()
				ffmpeg.Wait()
				ffmpeg = nil
				break
			}
			if n != packetSize {
				ffmpeg.Process.Kill()
				ffmpeg.Wait()
				ffmpeg = nil
				break
			}

			n, err = ffmpegStdin.Write(packet)
			if err != nil {
				ps.connectionControlChannel <- false
				break
			}
			if n < packetSize {
				ps.connectionControlChannel <- false
				break
			}
		}
	}
}

func sendVideoFrame(w http.ResponseWriter, req *http.Request, port int) {
	ps, ok := portMap[port]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if !config.Ports[port].Video || config.Ports[port].VideoExtension != "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	ps.videoFrameMutex.RLock()
	defer ps.videoFrameMutex.RUnlock()

	if len(ps.videoFrame) == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if req.Header.Get("Origin") != "" {
		w.Header().Set("Access-Control-Expose-Headers", "Device-Name, Width, Height")
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Device-Name", ps.deviceName)
	w.Header().Set("Width", strconv.Itoa(ps.initialVideoWidth))
	w.Header().Set("Height", strconv.Itoa(ps.initialVideoHeight))
	w.Write(ps.videoFrame)
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
