package main

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const (
	msgClientFull  = 0x1
	msgClientAudio = 0x2
	msgServerFull  = 0x9
	msgServerError = 0xf
	flagPositive   = 0x1
	flagNegative   = 0x3
	serialJSON     = 0x1
	compressGzip   = 0x1
)

type asrWireResponse struct {
	Code     int
	Last     bool
	Sequence int32
	Event    int32
	Payload  asrPayload
}

type asrPayload struct {
	AudioInfo struct {
		Duration int `json:"duration"`
	} `json:"audio_info"`
	Result struct {
		Text       string `json:"text"`
		Utterances []struct {
			Definite  bool   `json:"definite"`
			Text      string `json:"text"`
			StartTime int    `json:"start_time"`
			EndTime   int    `json:"end_time"`
		} `json:"utterances"`
	} `json:"result"`
	Error string `json:"error"`
}

func wireHeader(messageType, flags, serialization, compression byte) []byte {
	return []byte{0x11, (messageType << 4) | flags, (serialization << 4) | compression, 0x00}
}

func gzipBytes(input []byte) ([]byte, error) {
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	if _, err := w.Write(input); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func buildFullRequest(seq int32, payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	packed, err := gzipBytes(raw)
	if err != nil {
		return nil, err
	}
	buf := bytes.NewBuffer(wireHeader(msgClientFull, flagPositive, serialJSON, compressGzip))
	_ = binary.Write(buf, binary.BigEndian, seq)
	_ = binary.Write(buf, binary.BigEndian, uint32(len(packed)))
	_, _ = buf.Write(packed)
	return buf.Bytes(), nil
}

func buildAudioRequest(seq int32, audio []byte, last bool) ([]byte, error) {
	flags := byte(flagPositive)
	if last {
		flags = flagNegative
		if seq > 0 {
			seq = -seq
		}
	}
	packed, err := gzipBytes(audio)
	if err != nil {
		return nil, err
	}
	buf := bytes.NewBuffer(wireHeader(msgClientAudio, flags, 0, compressGzip))
	_ = binary.Write(buf, binary.BigEndian, seq)
	_ = binary.Write(buf, binary.BigEndian, uint32(len(packed)))
	_, _ = buf.Write(packed)
	return buf.Bytes(), nil
}

func parseASRResponse(msg []byte) (asrWireResponse, error) {
	var out asrWireResponse
	if len(msg) < 4 {
		return out, io.ErrUnexpectedEOF
	}
	headerBytes := int(msg[0]&0x0f) * 4
	if headerBytes < 4 || len(msg) < headerBytes {
		return out, fmt.Errorf("invalid header size")
	}
	messageType := msg[1] >> 4
	flags := msg[1] & 0x0f
	serialization := msg[2] >> 4
	compression := msg[2] & 0x0f
	payload := msg[headerBytes:]
	readI32 := func() (int32, error) {
		if len(payload) < 4 {
			return 0, io.ErrUnexpectedEOF
		}
		v := int32(binary.BigEndian.Uint32(payload[:4]))
		payload = payload[4:]
		return v, nil
	}
	if flags&0x01 != 0 {
		v, err := readI32()
		if err != nil {
			return out, err
		}
		out.Sequence = v
	}
	out.Last = flags&0x02 != 0
	if flags&0x04 != 0 {
		v, err := readI32()
		if err != nil {
			return out, err
		}
		out.Event = v
	}
	switch messageType {
	case msgServerFull:
		if len(payload) < 4 {
			return out, io.ErrUnexpectedEOF
		}
		size := int(binary.BigEndian.Uint32(payload[:4]))
		payload = payload[4:]
		if size > len(payload) {
			return out, io.ErrUnexpectedEOF
		}
		payload = payload[:size]
	case msgServerError:
		v, err := readI32()
		if err != nil {
			return out, err
		}
		out.Code = int(v)
		if len(payload) < 4 {
			return out, io.ErrUnexpectedEOF
		}
		size := int(binary.BigEndian.Uint32(payload[:4]))
		payload = payload[4:]
		if size <= len(payload) {
			payload = payload[:size]
		}
	default:
		return out, fmt.Errorf("unsupported server message type %d", messageType)
	}
	if len(payload) == 0 {
		return out, nil
	}
	if compression == compressGzip {
		r, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return out, err
		}
		payload, err = io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			return out, err
		}
	}
	if serialization == serialJSON {
		if err := json.Unmarshal(payload, &out.Payload); err != nil {
			return out, err
		}
	}
	return out, nil
}
