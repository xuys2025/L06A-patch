package main

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeSpeechProtocolTurn(t *testing.T) {
	dir := t.TempDir()
	serverPath := filepath.Join(dir, "speech.usock")
	clientPath := filepath.Join(dir, "mipns.usock")
	server, err := StartNativeSpeechServer(serverPath, func(string, ...any) {})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: clientPath, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	destination := &net.UnixAddr{Name: serverPath, Net: "unixgram"}

	register := testUpward(upRegister, 2, pbMessage(pbBytes(1, []byte("xiaomi")), pbBytes(2, []byte("pcm"))))
	if _, err := client.WriteToUnix(register, destination); err != nil {
		t.Fatal(err)
	}
	if got := readDownwardKind(t, client); got != downRegisterResponse {
		t.Fatalf("register response=%d", got)
	}
	if got := readDownwardKind(t, client); got != downEnableVoiceWakeup {
		t.Fatalf("enable wakeup=%d", got)
	}

	prepare := testUpward(upStreamPrepare, 3, pbMessage(pbVarint(1, activateWakeup), pbVarint(2, 1)))
	if _, err := client.WriteToUnix(prepare, destination); err != nil {
		t.Fatal(err)
	}
	if got := readDownwardKind(t, client); got != downStreamPrepareResponse {
		t.Fatalf("prepare response=%d", got)
	}

	var turn *NativeSpeechTurn
	select {
	case turn = <-server.Turns():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for turn")
	}
	if turn == nil || !turn.Wakeup {
		t.Fatalf("unexpected turn: %#v", turn)
	}

	pcm := []byte{1, 2, 3, 4, 5, 6}
	transmit := testUpward(upStreamTransmitting, 4, pbMessage(pbVarint(1, streamASR), pbBytes(2, pcm)))
	if _, err := client.WriteToUnix(transmit, destination); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-turn.Audio:
		if string(got) != string(pcm) {
			t.Fatalf("PCM=%v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for PCM")
	}

	if err := turn.StopCapture(); err != nil {
		t.Fatal(err)
	}
	if got := readDownwardKind(t, client); got != downStopCapture {
		t.Fatalf("stop response=%d", got)
	}
	if err := turn.Finish(false); err != nil {
		t.Fatal(err)
	}
	if got := readDownwardKind(t, client); got != downDialogFinish {
		t.Fatalf("finish response=%d", got)
	}
}

func TestNativeSpeechRejectsOpus(t *testing.T) {
	packet := testUpward(upRegister, 2, pbMessage(pbBytes(1, []byte("xiaomi")), pbBytes(2, []byte("opus32"))))
	event, err := decodeSpeechUpward(packet)
	if err != nil {
		t.Fatal(err)
	}
	if event.codec != "opus32" {
		t.Fatalf("codec=%q", event.codec)
	}
}

func TestCancelledTurnCannotStopReplacement(t *testing.T) {
	dir := t.TempDir()
	serverPath := filepath.Join(dir, "speech.usock")
	clientPath := filepath.Join(dir, "mipns.usock")
	server, err := StartNativeSpeechServer(serverPath, func(string, ...any) {})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: clientPath, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	destination := &net.UnixAddr{Name: serverPath, Net: "unixgram"}

	register := testUpward(upRegister, 2, pbMessage(pbBytes(1, []byte("xiaomi")), pbBytes(2, []byte("pcm"))))
	if _, err := client.WriteToUnix(register, destination); err != nil {
		t.Fatal(err)
	}
	readDownwardKind(t, client)
	readDownwardKind(t, client)

	prepare := testUpward(upStreamPrepare, 3, pbMessage(pbVarint(1, activateWakeup), pbVarint(2, 1)))
	if _, err := client.WriteToUnix(prepare, destination); err != nil {
		t.Fatal(err)
	}
	readDownwardKind(t, client)
	first := <-server.Turns()

	if _, err := client.WriteToUnix(testUpward(upStreamCancel, 0, nil), destination); err != nil {
		t.Fatal(err)
	}
	if _, err := client.WriteToUnix(prepare, destination); err != nil {
		t.Fatal(err)
	}
	readDownwardKind(t, client)
	second := <-server.Turns()
	if second.ID <= first.ID {
		t.Fatalf("replacement ID=%d, first ID=%d", second.ID, first.ID)
	}

	if err := first.StopCapture(); err != nil {
		t.Fatal(err)
	}
	if err := first.Finish(false); err != nil {
		t.Fatal(err)
	}
	if err := client.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	if _, _, err := client.ReadFromUnix(buf); err == nil || (!errors.Is(err, os.ErrDeadlineExceeded) && !isTimeout(err)) {
		t.Fatalf("stale turn emitted a control packet or unexpected error: %v", err)
	}
}

func TestNativeEmptyAudioError(t *testing.T) {
	if !isNativeEmptyAudioError(errors.New("ASR audio: audio source ended before producing PCM; source: speech.usock PCM")) {
		t.Fatal("expected empty PCM error to be recognized")
	}
	if isNativeEmptyAudioError(errors.New("ASR websocket unavailable")) {
		t.Fatal("unrelated ASR error was treated as empty audio")
	}
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func testUpward(kind, bodyField uint64, body []byte) []byte {
	up := pbMessage(pbVarint(1, kind))
	if bodyField != 0 {
		up = append(up, pbBytes(bodyField, body)...)
	}
	return pbMessage(pbVarint(1, speechTypeUpward), pbBytes(2, up))
}

func readDownwardKind(t *testing.T, conn *net.UnixConn) uint64 {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	n, _, err := conn.ReadFromUnix(buf)
	if err != nil {
		t.Fatal(err)
	}
	outer, err := parsePBFields(buf[:n])
	if err != nil {
		t.Fatal(err)
	}
	body, ok := pbFindBytes(outer, 3)
	if !ok {
		t.Fatal("downward packet has no body")
	}
	fields, err := parsePBFields(body)
	if err != nil {
		t.Fatal(err)
	}
	kind, ok := pbFindVarint(fields, 1)
	if !ok {
		t.Fatal("downward body has no type")
	}
	return kind
}
