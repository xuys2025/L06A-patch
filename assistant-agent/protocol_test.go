package main

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"io"
	"testing"
)

func TestBuildAudioRequestMarksFinalSequence(t *testing.T) {
	msg, err := buildAudioRequest(7, []byte{1, 2, 3, 4}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := msg[1] & 0x0f; got != flagNegative {
		t.Fatalf("flags=%d", got)
	}
	if got := int32(binary.BigEndian.Uint32(msg[4:8])); got != -7 {
		t.Fatalf("sequence=%d", got)
	}
	size := int(binary.BigEndian.Uint32(msg[8:12]))
	r, err := gzip.NewReader(bytes.NewReader(msg[12 : 12+size]))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, []byte{1, 2, 3, 4}) {
		t.Fatalf("payload=%v", raw)
	}
}

func TestParseASRDefiniteResult(t *testing.T) {
	payload := asrPayload{}
	payload.Result.Text = "播放海阔天空"
	payload.Result.Utterances = append(payload.Result.Utterances, struct {
		Definite  bool   `json:"definite"`
		Text      string `json:"text"`
		StartTime int    `json:"start_time"`
		EndTime   int    `json:"end_time"`
	}{Definite: true, Text: "播放海阔天空", StartTime: 0, EndTime: 1200})
	raw, _ := json.Marshal(payload)
	packed, _ := gzipBytes(raw)
	buf := bytes.NewBuffer(wireHeader(msgServerFull, flagNegative, serialJSON, compressGzip))
	_ = binary.Write(buf, binary.BigEndian, int32(-3))
	_ = binary.Write(buf, binary.BigEndian, uint32(len(packed)))
	buf.Write(packed)
	got, err := parseASRResponse(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Last || got.Payload.Result.Text != "播放海阔天空" || !got.Payload.Result.Utterances[0].Definite {
		t.Fatalf("unexpected: %#v", got)
	}
}

func TestParseMusicIntent(t *testing.T) {
	cases := []struct{ input, action, query string }{
		{"播放海阔天空", "play", "海阔天空"},
		{"下一首", "next", ""},
		{"暂停播放", "pause", ""},
	}
	for _, tc := range cases {
		got, ok := ParseMusicIntent(tc.input)
		if !ok || got.Action != tc.action || got.Query != tc.query {
			t.Fatalf("%q -> %#v, %v", tc.input, got, ok)
		}
	}
	got, ok := ParseMusicIntent("把音量调到35")
	if !ok || got.Action != "volume" || got.Volume != 35 {
		t.Fatalf("volume -> %#v, %v", got, ok)
	}
}
