package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const nativeSpeechSocketPath = "/tmp/mico_aivs_lab/usock/speech.usock"

const (
	speechTypeUpward   = uint64(0)
	speechTypeDownward = uint64(1)

	upRegister           = uint64(0)
	upStreamPrepare      = uint64(1)
	upStreamCancel       = uint64(2)
	upStreamTransmitting = uint64(3)
	upStreamEnd          = uint64(4)

	downRegisterResponse      = uint64(0)
	downStreamPrepareResponse = uint64(1)
	downStopCapture           = uint64(2)
	downExpectSpeech          = uint64(3)
	downDialogFinish          = uint64(5)
	downEnableVoiceWakeup     = uint64(9)

	streamASR      = uint64(2)
	activateWakeup = uint64(0)
)

type NativeSpeechTurn struct {
	ID              uint64
	Wakeup          bool
	InteractionMode uint64
	Audio           <-chan []byte

	server    *NativeSpeechServer
	peer      *net.UnixAddr
	dialogID  string
	audioCh   chan []byte
	closeOne  sync.Once
	mu        sync.Mutex
	finished  bool
	cancelled bool
}

func (t *NativeSpeechTurn) closeAudio() { t.closeOne.Do(func() { close(t.audioCh) }) }

func (t *NativeSpeechTurn) cancel() {
	t.mu.Lock()
	t.cancelled = true
	t.mu.Unlock()
	t.closeAudio()
}

func (t *NativeSpeechTurn) StopCapture() error {
	t.mu.Lock()
	cancelled := t.cancelled
	t.mu.Unlock()
	if cancelled {
		return nil
	}
	return t.server.sendTurnDownward(t, downStopCapture, 0, nil)
}

func (t *NativeSpeechTurn) Finish(continueDialog bool) error {
	t.mu.Lock()
	if t.finished || t.cancelled {
		t.mu.Unlock()
		return nil
	}
	t.finished = true
	t.mu.Unlock()
	if continueDialog {
		expect := pbMessage(pbBool(2, false))
		if err := t.server.sendTurnDownward(t, downExpectSpeech, 4, expect); err != nil {
			return err
		}
	}
	finish := pbMessage(
		pbBool(1, false),
		pbBool(2, continueDialog),
		pbBool(3, continueDialog),
		pbBool(4, !continueDialog),
		pbVarint(5, t.server.chatCount()),
		pbVarint(6, t.server.dialogCount()),
	)
	return t.server.sendTurnDownward(t, downDialogFinish, 6, finish)
}

type NativeSpeechServer struct {
	conn          *net.UnixConn
	path          string
	turns         chan *NativeSpeechTurn
	logf          func(string, ...any)
	done          chan struct{}
	mu            sync.Mutex
	peer          string
	current       *NativeSpeechTurn
	nextID        uint64
	chats         uint64
	dialogs       uint64
	beforePrepare func(bool) error
}

func StartNativeSpeechServer(path string, logf func(string, ...any), prepareHooks ...func(bool) error) (*NativeSpeechServer, error) {
	if path == "" {
		path = nativeSpeechSocketPath
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create native speech socket directory: %w", err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove stale native speech socket: %w", err)
	}
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		return nil, fmt.Errorf("listen native speech socket: %w", err)
	}
	server := &NativeSpeechServer{
		conn: conn, path: path, turns: make(chan *NativeSpeechTurn, 4),
		logf: logf, done: make(chan struct{}),
	}
	if len(prepareHooks) > 0 {
		server.beforePrepare = prepareHooks[0]
	}
	go server.serve()
	logf("native speech socket listening path=%s", path)
	return server, nil
}

func (s *NativeSpeechServer) Turns() <-chan *NativeSpeechTurn { return s.turns }

func (s *NativeSpeechServer) Close() error {
	select {
	case <-s.done:
		return nil
	default:
		close(s.done)
	}
	err := s.conn.Close()
	_ = os.Remove(s.path)
	return err
}

func (s *NativeSpeechServer) RequestCapture() error {
	out, err := runCommand("/bin/ubus", "-t", "1", "-S", "call", "pnshelper", "event_notify", `{"src":1,"event":0}`)
	if err != nil {
		return err
	}
	if !strings.Contains(out, `"code":0`) && !strings.Contains(out, `"code": 0`) {
		return fmt.Errorf("pnshelper rejected capture: %s", strings.TrimSpace(out))
	}
	return nil
}

func (s *NativeSpeechServer) chatCount() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chats
}

func (s *NativeSpeechServer) dialogCount() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dialogs
}

func (s *NativeSpeechServer) serve() {
	defer close(s.turns)
	buf := make([]byte, 256*1024)
	for {
		n, peer, err := s.conn.ReadFromUnix(buf)
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				s.logf("native speech receive failed: %v", err)
				return
			}
		}
		event, err := decodeSpeechUpward(buf[:n])
		if err != nil {
			s.logf("native speech malformed packet bytes=%d error=%v", n, err)
			continue
		}
		if event == nil {
			continue
		}
		if err := s.handleUpward(peer, event); err != nil {
			s.logf("native speech packet failed type=%d error=%v", event.kind, err)
		}
	}
}

type speechUpEvent struct {
	kind            uint64
	vendor          string
	codec           string
	activation      uint64
	interactionMode uint64
	streamType      uint64
	data            []byte
}

func (s *NativeSpeechServer) handleUpward(peer *net.UnixAddr, event *speechUpEvent) error {
	s.mu.Lock()
	registeredPeer := s.peer
	s.mu.Unlock()
	if event.kind != upRegister && registeredPeer != "" && peer.Name != registeredPeer {
		return fmt.Errorf("packet from unregistered peer %q", peer.Name)
	}
	switch event.kind {
	case upRegister:
		codec := strings.ToLower(strings.TrimSpace(event.codec))
		if codec != "" && codec != "pcm" && codec != "pcm16" && codec != "s16le" {
			return fmt.Errorf("unsupported codec %q; pns must start without -r opus32", event.codec)
		}
		s.mu.Lock()
		if s.current != nil {
			s.current.cancel()
		}
		s.current = nil
		s.peer = peer.Name
		s.chats = 0
		s.mu.Unlock()
		if err := s.sendDownward(peer, downRegisterResponse, "", 0, nil); err != nil {
			return err
		}
		if err := s.sendDownward(peer, downEnableVoiceWakeup, "", 0, nil); err != nil {
			return err
		}
		s.logf("native speech registered vendor=%s codec=%s peer=%s", event.vendor, emptyDefault(event.codec, "pcm16"), peer.Name)
	case upStreamPrepare:
		if s.beforePrepare != nil {
			if err := s.beforePrepare(event.activation == activateWakeup); err != nil {
				return err
			}
		}
		dialogID, err := newDialogID()
		if err != nil {
			return err
		}
		s.mu.Lock()
		if s.current != nil {
			s.current.cancel()
		}
		s.nextID++
		if event.activation == activateWakeup {
			s.dialogs++
			s.chats = 1
		} else {
			s.chats++
			if s.chats == 0 {
				s.chats = 1
			}
		}
		audioCh := make(chan []byte, 2048)
		turn := &NativeSpeechTurn{
			ID: s.nextID, Wakeup: event.activation == activateWakeup,
			InteractionMode: event.interactionMode, Audio: audioCh,
			server: s, peer: &net.UnixAddr{Name: peer.Name, Net: "unixgram"},
			dialogID: dialogID, audioCh: audioCh,
		}
		s.current = turn
		s.mu.Unlock()
		connected := pbMessage(pbBool(1, true))
		if err := s.sendDownward(peer, downStreamPrepareResponse, dialogID, 3, connected); err != nil {
			return err
		}
		s.logf("native speech turn prepared id=%d wakeup=%t mode=%d", turn.ID, turn.Wakeup, turn.InteractionMode)
		select {
		case s.turns <- turn:
		default:
			turn.closeAudio()
			return errors.New("native speech turn queue is full")
		}
	case upStreamTransmitting:
		if event.streamType != streamASR {
			return nil
		}
		s.mu.Lock()
		turn := s.current
		s.mu.Unlock()
		if turn == nil {
			return nil
		}
		data := append([]byte(nil), event.data...)
		select {
		case turn.audioCh <- data:
		default:
			turn.closeAudio()
			return fmt.Errorf("native speech audio queue overflow turn=%d", turn.ID)
		}
	case upStreamCancel, upStreamEnd:
		s.mu.Lock()
		turn := s.current
		s.current = nil
		s.mu.Unlock()
		if turn != nil {
			if event.kind == upStreamCancel {
				turn.cancel()
			} else {
				turn.closeAudio()
			}
			s.logf("native speech turn closed id=%d event=%d", turn.ID, event.kind)
		}
	}
	return nil
}

func (s *NativeSpeechServer) sendTurnDownward(turn *NativeSpeechTurn, kind uint64, bodyField uint64, body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if turn.ID != s.nextID {
		s.logf("native speech ignored stale control turn=%d latest=%d type=%d", turn.ID, s.nextID, kind)
		return nil
	}
	return s.sendDownward(turn.peer, kind, turn.dialogID, bodyField, body)
}

func (s *NativeSpeechServer) sendDownward(peer *net.UnixAddr, kind uint64, dialogID string, bodyField uint64, body []byte) error {
	parts := [][]byte{pbVarint(1, kind)}
	if dialogID != "" {
		parts = append(parts, pbBytes(2, []byte(dialogID)))
	}
	if bodyField != 0 {
		parts = append(parts, pbBytes(bodyField, body))
	}
	down := pbMessage(parts...)
	packet := pbMessage(pbVarint(1, speechTypeDownward), pbBytes(3, down))
	if _, err := s.conn.WriteToUnix(packet, peer); err != nil {
		return fmt.Errorf("send native speech response type=%d: %w", kind, err)
	}
	return nil
}

func newDialogID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("dialog id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func emptyDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func decodeSpeechUpward(packet []byte) (*speechUpEvent, error) {
	outer, err := parsePBFields(packet)
	if err != nil {
		return nil, err
	}
	if value, ok := pbFindVarint(outer, 1); !ok || value != speechTypeUpward {
		return nil, nil
	}
	body, ok := pbFindBytes(outer, 2)
	if !ok {
		return nil, errors.New("upward speech packet has no body")
	}
	fields, err := parsePBFields(body)
	if err != nil {
		return nil, err
	}
	kind, ok := pbFindVarint(fields, 1)
	if !ok {
		return nil, errors.New("upward speech body has no type")
	}
	event := &speechUpEvent{kind: kind}
	switch kind {
	case upRegister:
		if raw, ok := pbFindBytes(fields, 2); ok {
			inner, err := parsePBFields(raw)
			if err != nil {
				return nil, err
			}
			event.vendor = stringPB(inner, 1)
			event.codec = stringPB(inner, 2)
		}
	case upStreamPrepare:
		if raw, ok := pbFindBytes(fields, 3); ok {
			inner, err := parsePBFields(raw)
			if err != nil {
				return nil, err
			}
			event.activation, _ = pbFindVarint(inner, 1)
			event.interactionMode, _ = pbFindVarint(inner, 2)
		}
	case upStreamTransmitting:
		if raw, ok := pbFindBytes(fields, 4); ok {
			inner, err := parsePBFields(raw)
			if err != nil {
				return nil, err
			}
			event.streamType, _ = pbFindVarint(inner, 1)
			event.data, _ = pbFindBytes(inner, 2)
		}
	}
	return event, nil
}

type pbField struct {
	number uint64
	wire   uint64
	value  uint64
	bytes  []byte
}

func parsePBFields(buf []byte) ([]pbField, error) {
	fields := make([]pbField, 0, 8)
	for len(buf) > 0 {
		key, rest, err := decodePBVarint(buf)
		if err != nil {
			return nil, err
		}
		buf = rest
		number, wire := key>>3, key&7
		if number == 0 {
			return nil, errors.New("protobuf field zero")
		}
		switch wire {
		case 0:
			value, rest, err := decodePBVarint(buf)
			if err != nil {
				return nil, err
			}
			buf = rest
			fields = append(fields, pbField{number: number, wire: wire, value: value})
		case 1:
			if len(buf) < 8 {
				return nil, errors.New("truncated protobuf fixed64")
			}
			buf = buf[8:]
		case 2:
			length, rest, err := decodePBVarint(buf)
			if err != nil {
				return nil, err
			}
			buf = rest
			if length > uint64(len(buf)) {
				return nil, errors.New("truncated protobuf bytes")
			}
			value := append([]byte(nil), buf[:int(length)]...)
			buf = buf[int(length):]
			fields = append(fields, pbField{number: number, wire: wire, bytes: value})
		case 5:
			if len(buf) < 4 {
				return nil, errors.New("truncated protobuf fixed32")
			}
			buf = buf[4:]
		default:
			return nil, fmt.Errorf("unsupported protobuf wire type %d", wire)
		}
	}
	return fields, nil
}

func pbFindVarint(fields []pbField, number uint64) (uint64, bool) {
	for _, field := range fields {
		if field.number == number && field.wire == 0 {
			return field.value, true
		}
	}
	return 0, false
}

func pbFindBytes(fields []pbField, number uint64) ([]byte, bool) {
	for _, field := range fields {
		if field.number == number && field.wire == 2 {
			return field.bytes, true
		}
	}
	return nil, false
}

func stringPB(fields []pbField, number uint64) string {
	value, _ := pbFindBytes(fields, number)
	return string(value)
}

func pbMessage(parts ...[]byte) []byte {
	var out []byte
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

func pbBool(number uint64, value bool) []byte {
	if value {
		return pbVarint(number, 1)
	}
	return pbVarint(number, 0)
}

func pbVarint(number, value uint64) []byte {
	out := encodePBVarint(number << 3)
	out = append(out, encodePBVarint(value)...)
	return out
}

func pbBytes(number uint64, value []byte) []byte {
	out := encodePBVarint(number<<3 | 2)
	out = append(out, encodePBVarint(uint64(len(value)))...)
	out = append(out, value...)
	return out
}

func encodePBVarint(value uint64) []byte {
	var out []byte
	for value >= 0x80 {
		out = append(out, byte(value)|0x80)
		value >>= 7
	}
	return append(out, byte(value))
}

func decodePBVarint(buf []byte) (uint64, []byte, error) {
	var value uint64
	for shift := uint(0); shift < 64; shift += 7 {
		if len(buf) == 0 {
			return 0, nil, errors.New("truncated protobuf varint")
		}
		b := buf[0]
		buf = buf[1:]
		value |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return value, buf, nil
		}
	}
	return 0, nil, errors.New("protobuf varint too long")
}
